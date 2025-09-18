package addon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adrg/strutil/metrics"
	"github.com/dbytex91/streamx/internal/cinemeta"
	"github.com/dbytex91/streamx/internal/debrid/realdebrid"
	"github.com/dbytex91/streamx/internal/model"
	"github.com/dbytex91/streamx/internal/pipe"
	"github.com/dbytex91/streamx/internal/prowlarr"
	"github.com/dbytex91/streamx/internal/titleparser"
	"github.com/coocood/freecache"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
)

const (
	cacheSize           = 50 * 1024 * 1024 // 50MB
	downloadURLExpiry   = 5 * 60
	maxTitleDistance    = 5
	maxStreamsResult    = 5
	infoHashCacheExpiry = 24 * 60 * 60 // 1 day
	maxSizeInBytes      = 30 * 1 << 30 // 30GB
	minSizeInBytes      = 100 * 1 << 20 // 100MB
)

var (
	mediaContainerExtensions = []string{
		"mkv",
		"mk3d",
		"mp4",
		"m4v",
		"mov",
		"avi",
	}

	nonWordCharacter = regexp.MustCompile(`[^a-zA-Z0-9]+`)

	resolutionName = map[int]string{
		2160: "4K",
		1080: "1080p",
		720:  "720p",
		480:  "480p",
		360:  "360p",
		0:    "Unknown",
	}

	avoidQualities = []string{
		"cam", "camrip", "telesync", "tsrip", "hdcam", "tc", "ppvrip", "r5", "vhsscr",
	}
)

// Addon implements a Stremio addon
type Addon struct {
	id          string
	name        string
	version     string
	description string

	cinemetaClient *cinemeta.CineMeta
	prowlarrClient *prowlarr.Prowlarr
	prowlarrURL    string
	prowlarrAPIKey string
	realDebridClient *realdebrid.RealDebrid
	realDebridAPIKey string
	cache          *freecache.Cache
}

type Option func(*Addon)

type GetStreamsResponse struct {
	Streams []StreamItem `json:"streams"`
}

type streamRecord struct {
	ContentType    ContentType
	ID             string
	Season         int
	Episode        int
	BaseURL        string
	RemoteAddress  string
	MetaInfo       *model.MetaInfo
	TitleInfo      *titleparser.MetaInfo
	Indexer        *prowlarr.Indexer
	Torrent        *prowlarr.Torrent
	Files          []*realdebrid.File
	MediaFile      *realdebrid.File
	SearchBySeason bool
	RDClient       *realdebrid.RealDebrid
	Prowlarr       *prowlarr.Prowlarr
	UserData       *UserData
}

func New(opts ...Option) *Addon {
	addon := &Addon{
		description:    "Advanced torrent streaming addon requiring Prowlarr or Real Debrid",
		cinemetaClient: cinemeta.New(),
		cache:          freecache.NewCache(cacheSize),
	}

	for _, opt := range opts {
		opt(addon)
	}

	// At least one service (Prowlarr or Real Debrid) must be configured via environment variables
	// If none are configured via environment, users can still configure via UI
	if addon.prowlarrClient == nil && addon.realDebridClient == nil {
		log.Warn("No services configured via environment variables. Users must configure via UI.")
	}

	return addon
}

func (add *Addon) HandleGetManifest(c *fiber.Ctx) error {
	// Check if this is the base route (/manifest.json) or userData route (/:userData/manifest.json)
	userDataRaw := c.Params("userData")
	
	var configRequired bool
	
	if userDataRaw == "" {
		// Base route - only require configuration if no environment clients are available
		configRequired = add.prowlarrClient == nil && add.realDebridClient == nil
	} else {
		// userData route - try to parse userData
		_, err := parseUserData(add, c)
		configRequired = err != nil
	}

	manifest := &Manifest{
		ID:          add.id,
		Name:        add.name,
		Description: add.description,
		Version:     add.version,
		ResourceItems: []ResourceItem{
			{
				Name:       ResourceStream,
				Types:      []ContentType{ContentTypeMovie, ContentTypeSeries},
				IDPrefixes: []string{"tt"},
			},
		},
		Types:      []ContentType{ContentTypeMovie, ContentTypeSeries},
		Catalogs:   []CatalogItem{},
		IDPrefixes: []string{"tt"},
		Logo:       c.BaseURL() + "/logo",
		BehaviorHints: &BehaviorHints{
			Configurable:          true,
			ConfigurationRequired: configRequired,
		},
	}

	return c.JSON(manifest)
}

func (add *Addon) HandleLogo(c *fiber.Ctx) error {
	// Set content type for SVG
	c.Set("Content-Type", "image/svg+xml")
	c.Set("Cache-Control", "public, max-age=86400") // Cache for 24 hours
	
	// Serve the static logo file
	return c.SendFile("/bin/logo.svg")
}

func (add *Addon) HandleDownload(c *fiber.Ctx) error {
	infoHash := strings.ToLower(c.Params("infoHash"))
	fileID := strings.ToLower(c.Params("fileID"))
	ipAddress := getIPAddress(c)
	
	// Check if this is the base route (/download/...) or userData route (/:userData/download/...)
	userDataRaw := c.Params("userData")
	
	var userData *UserData
	var err error
	
	if userDataRaw == "" {
		// Base route - use environment configuration if available
		if add.realDebridClient != nil {
			log.Infof("Using environment configuration for download with default settings")
			userData = NewUserDataWithDefaults()
		} else {
			return c.Status(400).JSON(fiber.Map{
				"error": "Download requires Real Debrid configuration.",
			})
		}
	} else {
		// userData route - parse userData
		userData, err = parseUserData(add, c)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": "Invalid configuration data.",
			})
		}
	}

	// Use final configuration values (environment + UI overrides)
	finalRealDebridAPIKey := userData.RDAPIKey
	if finalRealDebridAPIKey == "" && add.realDebridAPIKey != "" {
		finalRealDebridAPIKey = add.realDebridAPIKey
	}
	
	var realDebrid *realdebrid.RealDebrid
	if finalRealDebridAPIKey != "" {
		realDebrid = realdebrid.New(finalRealDebridAPIKey, ipAddress)
	}

	if realDebrid == nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "This addon is configured for native torrent streaming. Download URLs are not available without Real Debrid. Please use the stream directly in Stremio.",
		})
	}

	var downloadURL string
	rawDownloadURL, err := add.cache.Get([]byte(userData.RDAPIKey + infoHash + fileID))
	if err != nil {
		downloadURL, err = realDebrid.GetDownloadByInfoHash(infoHash, fileID)
		if err != nil {
			log.WithContext(c.Context()).Errorf("Couldn't generate the download link for %s, %s: %v", infoHash, fileID, err)
			return err
		}

		err = add.cache.Set([]byte(userData.RDAPIKey+infoHash+fileID), []byte(downloadURL), downloadURLExpiry)
		if err != nil {
			log.WithContext(c.Context()).Warnf("Failed to cache downloadURL: %v", err)
		}
	} else {
		downloadURL = string(rawDownloadURL)
	}

	c.Response().Header.Add("Cache-control", "max-age=86400, public")
	return c.Redirect(downloadURL)
}

func (add *Addon) HandleGetStreams(c *fiber.Ctx) error {
	// Check if this is the base route (/stream/...) or userData route (/:userData/stream/...)
	userDataRaw := c.Params("userData")
	
	var userData *UserData
	var err error
	
	if userDataRaw == "" {
		// Base route - use environment configuration if available
		if add.prowlarrClient != nil || add.realDebridClient != nil {
			log.Infof("Using environment configuration with optimized default settings")
			userData = NewUserDataWithDefaults()
		} else {
			return c.Status(400).JSON(fiber.Map{
				"error": "Configuration required. Please configure the addon.",
			})
		}
	} else {
		// userData route - parse userData
		userData, err = parseUserData(add, c)
		if err != nil {
			log.Errorf("Failed to parse user data: %v", err)
			return c.Status(400).JSON(fiber.Map{
				"error": "Invalid configuration data.",
			})
		}
	}
	
	compiled := regexp.MustCompile(`/stream/(movie|series).+$`)
	p := pipe.New(add.sourceFromContextWithUserData(c, userData))

	p.Map(add.fetchMetaInfo)
	p.FanOut(add.fanOutToAllIndexers)
	p.Channel(add.searchForTorrents)
	p.Map(add.parseTorrentTitle)
	p.Filter(add.createExcludeTorrentsFilter())
	// Sorting is now handled in groupByResolution
	p.FanOut(add.enrichInfoHash, pipe.Concurrency[streamRecord](10))
	p.Filter(deduplicateTorrent())
	p.Batch(add.enrichWithCachedFiles)
	p.FanOut(add.locateMediaFile)

	// Get user-configured timeout
	timeoutSeconds, err := strconv.Atoi(userData.SearchTimeout)
	if err != nil || timeoutSeconds < 10 || timeoutSeconds > 120 {
		timeoutSeconds = 45 // Default fallback
	}
	
	records := add.sinkResultsWithTimeout(p, time.Duration(timeoutSeconds)*time.Second)
	
	// Apply user-selected sorting method
	sortMethod := userData.SortMethod
	if sortMethod == "" {
		sortMethod = "quality" // Default to quality score
	}
	
	if sortMethod == "quality" {
		records = sortByQualityScore(records)
		log.Infof("Pipeline completed - Processing %d total records (sorted by quality score)", len(records))
	} else {
		records = groupByResolution(records, 3)
		log.Infof("Pipeline completed - Processing %d total records (sorted by resolution diversity)", len(records))
	}
	results := make([]StreamItem, 0, maxStreamsResult)
	for _, r := range records {
		var streamItem StreamItem
		
		if r.RDClient != nil && len(r.Files) > 0 {
			// Use debrid download URL (existing behavior)
			streamURL := r.BaseURL + compiled.ReplaceAllString(c.Path(), "/download/"+r.Torrent.InfoHash+"/"+r.MediaFile.ID)
			streamItem = StreamItem{
				Name:  formatStreamName(r.TitleInfo, true), // Cached = true for debrid
				Title: formatStreamTitle(r.TitleInfo, r.MediaFile.FileSize, r.Torrent.Seeders, r.Indexer.Name, true),
				URL:   streamURL,
				BehaviorHints: &StreamBehaviorHints{
					VideoSize: r.MediaFile.FileSize,
					FileName:  path.Base(r.MediaFile.FileName),
				},
			}
		} else {
			// Use native Stremio torrent streaming (when no debrid or debrid failed)
			streamItem = StreamItem{
				Name:     formatStreamName(r.TitleInfo, false), // Cached = false for native torrenting
				Title:    formatStreamTitle(r.TitleInfo, r.MediaFile.FileSize, r.Torrent.Seeders, r.Indexer.Name, false),
				InfoHash: r.Torrent.InfoHash,
				FileIndex: 0, // For now, use first file. Could be enhanced to find specific media file
				BehaviorHints: &StreamBehaviorHints{
					VideoSize: r.MediaFile.FileSize,
					FileName:  path.Base(r.MediaFile.FileName),
				},
			}
		}

		results = append(results, streamItem)

		if len(results) == maxStreamsResult {
			break
		}
	}

	c.Response().Header.Add("Cache-control", "max-age=1800, public, stale-while-revalidate=604800, stale-if-error=604800")
	return c.JSON(GetStreamsResponse{
		Streams: results,
	})
}

func (add *Addon) sourceFromContext(c *fiber.Ctx) func() ([]*streamRecord, error) {
	return func() ([]*streamRecord, error) {
		userData, err := parseUserData(add, c)
		if err != nil {
			return nil, errors.New("invalid user data")
		}
		return add.sourceFromContextWithUserData(c, userData)()
	}
}

func (add *Addon) sourceFromContextWithUserData(c *fiber.Ctx, userData *UserData) func() ([]*streamRecord, error) {
	return func() ([]*streamRecord, error) {
		ipAddress := getIPAddress(c)

		// Use final configuration values (environment + UI overrides)
		finalProwlarrURL := userData.ProwlarrURL
		finalProwlarrAPIKey := userData.ProwlarrAPIKey
		finalRealDebridAPIKey := userData.RDAPIKey
		
		// If UI values are empty, use environment variables
		if finalProwlarrURL == "" && add.prowlarrURL != "" {
			finalProwlarrURL = add.prowlarrURL
		}
		if finalProwlarrAPIKey == "" && add.prowlarrAPIKey != "" {
			finalProwlarrAPIKey = add.prowlarrAPIKey
		}
		if finalRealDebridAPIKey == "" && add.realDebridAPIKey != "" {
			finalRealDebridAPIKey = add.realDebridAPIKey
		}
		
		var realDebrid *realdebrid.RealDebrid
		if finalRealDebridAPIKey != "" {
			realDebrid = realdebrid.New(finalRealDebridAPIKey, ipAddress)
		}
		
		var prowlarrClient *prowlarr.Prowlarr
		if finalProwlarrURL != "" && finalProwlarrAPIKey != "" {
			prowlarrClient = prowlarr.New(finalProwlarrURL, finalProwlarrAPIKey)
		}

		id := c.Params("id")
		season := 0
		episode := 0
		contentType := ContentType(c.Params("type"))
		if contentType == ContentTypeSeries {
			tokens := strings.Split(id, "%3A")
			if len(tokens) != 3 {
				return nil, errors.New("invalid stremio id")
			}
			id = tokens[0]
			season, _ = strconv.Atoi(tokens[1])
			episode, _ = strconv.Atoi(tokens[2])
		}

		return []*streamRecord{{
			ContentType:   contentType,
			ID:            id,
			Season:        season,
			Episode:       episode,
			BaseURL:       c.BaseURL(),
			RemoteAddress: c.Context().RemoteIP().String(),
			RDClient:      realDebrid,
			Prowlarr:      prowlarrClient,
			UserData:      userData, // Add user data to stream record
		}}, nil
	}
}

func (add *Addon) fetchMetaInfo(r *streamRecord) (*streamRecord, error) {
	switch r.ContentType {
	case ContentTypeMovie:
		resp, err := add.cinemetaClient.GetMovieById(r.ID)
		if err != nil {
			return r, err
		}

		r.MetaInfo = resp
		return r, nil
	case ContentTypeSeries:
		resp, err := add.cinemetaClient.GetSeriesById(r.ID)
		if err != nil {
			return r, err
		}

		r.MetaInfo = resp
		return r, nil
	default:
		return r, errors.New("not supported content type")
	}
}

func (add *Addon) fanOutToAllIndexers(r *streamRecord) ([]*streamRecord, error) {
	allIndexers, err := r.Prowlarr.GetAllIndexers()
	if err != nil {
		return nil, fmt.Errorf("couldn't load all indexers: %v", err)
	}

	records := make([]*streamRecord, 0, len(allIndexers))
	for _, indexer := range allIndexers {
		if !indexer.Enable {
			log.Infof("Skip %s as it's disabled", indexer.Name)
			continue
		}

		newR := *r
		newR.Indexer = indexer
		records = append(records, &newR)
	}

	return records, nil
}

func (add *Addon) searchForTorrents(r *streamRecord, stopCh <-chan struct{}, outCh chan<- *streamRecord) error {
	var torrents []*prowlarr.Torrent
	var err error
	totalRecords := 0

	isStopped := func() bool {
		select {
		case <-stopCh:
			return true
		default:
			return false
		}
	}

	sendAllRecords := func(torrents []*prowlarr.Torrent) {
		totalRecords += len(torrents)
		for _, torrent := range torrents {
			newRecord := *r
			newRecord.Torrent = torrent
			pipe.SendRecords([]*streamRecord{&newRecord}, outCh, stopCh)
			if isStopped() {
				return
			}
		}
	}

	switch r.ContentType {
	case ContentTypeMovie:
		torrents, err = r.Prowlarr.SearchMovieTorrents(r.Indexer, r.MetaInfo.Name)
		if err != nil {
			return nil
		}

		sendAllRecords(torrents)
	case ContentTypeSeries:
		torrents, err = r.Prowlarr.SearchSeriesTorrents(r.Indexer, r.MetaInfo.Name)
		if err != nil {
			return nil
		}

		sendAllRecords(torrents)
		if !isStopped() && len(torrents) == r.Indexer.Capabilities.LimitDefaults && r.Indexer.Capabilities.LimitDefaults > 0 {
			torrents, _ = r.Prowlarr.SearchSeasonTorrents(r.Indexer, r.MetaInfo.Name, r.Season)
			sendAllRecords(torrents)
		}
	}

	log.Infof("Completed search from %s - Found %d torrents", r.Indexer.Name, totalRecords)
	return nil
}

func (add *Addon) enrichInfoHash(r *streamRecord) ([]*streamRecord, error) {
	var err error

	if r.Torrent.InfoHash == "" {
		infoHash, err := add.cache.Get(r.Torrent.GID)
		if err == nil {
			r.Torrent.InfoHash = string(infoHash)
		}
	}

	r.Torrent, err = r.Prowlarr.FetchInfoHash(r.Torrent)
	if err != nil {
		log.Errorf("Failed to fetch InfoHash for %s due to: %v", r.Torrent.Guid, err)
		return nil, nil
	}

	if r.Torrent.InfoHash == "" {
		log.Warnf("Unable to find InfoHash for %s", r.Torrent.Guid)
		return nil, nil
	}

	err = add.cache.Set(r.Torrent.GID, []byte(r.Torrent.InfoHash), infoHashCacheExpiry)
	if err != nil {
		log.Errorf("Failed to cache the InfoHash due to: %v", err)
		return nil, nil
	}

	return []*streamRecord{r}, nil
}

func (add *Addon) enrichWithCachedFiles(records []*streamRecord) ([]*streamRecord, error) {
	if len(records) == 0 {
		return records, nil
	}

	// If no debrid client is available, skip debrid entirely and return all records
	if records[0].RDClient == nil {
		return records, nil
	}

	// Only search debrid if client is available
	infoHashs := make([]string, 0, len(records))
	for _, record := range records {
		if record.Torrent.InfoHash == "" {
			log.Infof("Skipped %s due to missing infoHash", record.Torrent.Title)
			continue
		}

		infoHashs = append(infoHashs, record.Torrent.InfoHash)
	}

	filesByHash, err := records[0].RDClient.GetFiles(infoHashs)
	if err != nil {
		log.Errorf("Failed to fetch files from debrid: %v", err)
		// Return empty files but keep the torrents for native streaming
		log.Infof("Debrid failed, returning %d records for native torrent streaming", len(records))
		return records, nil
	}

	cachedRecords := make([]*streamRecord, 0, len(records))
	for _, r := range records {
		if files, ok := filesByHash[r.Torrent.InfoHash]; ok {
			newR := *r
			newR.Files = files
			cachedRecords = append(cachedRecords, &newR)
		}
	}

	log.Infof("Found %d cached from %d records", len(cachedRecords), len(records))
	return cachedRecords, nil
}

func (add *Addon) sinkResults(p *pipe.Pipe[streamRecord]) []*streamRecord {
	return add.sinkResultsWithTimeout(p, 45*time.Second)
}

func (add *Addon) sinkResultsWithTimeout(p *pipe.Pipe[streamRecord], timeout time.Duration) []*streamRecord {
	records := []*streamRecord{}
	err := p.SinkWithTimeout(func(r *streamRecord) error {
		records = append(records, r)
		return nil
	}, timeout)

	if err != nil {
		log.Errorf("Error while processing: %v", err)
	}

	return records
}

// sortByQualityScore sorts all torrents by a weighted quality score for optimal speed/quality balance
func sortByQualityScore(records []*streamRecord) []*streamRecord {
	slices.SortFunc(records, func(r1, r2 *streamRecord) int {
		score1 := calculateQualityScore(r1)
		score2 := calculateQualityScore(r2)
		
		// Sort by quality score (descending)
		if score1 > score2 {
			return -1
		}
		if score1 < score2 {
			return 1
		}
		return 0
	})
	
	return records
}

// calculateQualityScore returns a weighted score for optimal speed/quality balance
func calculateQualityScore(r *streamRecord) float64 {
	// Resolution score (0-100) - Visual quality
	resolutionScore := float64(r.TitleInfo.Resolution) / 21.6 // 2160p = 100
	if resolutionScore > 100 {
		resolutionScore = 100
	}
	
	// Source quality score (0-100) - Encoding quality
	sourceScore := float64(getQualityScore(r.TitleInfo.Quality)) * 10 // Convert 1-10 to 10-100
	
	// File size score (0-100) - Optimal sizes get highest scores
	sizeGB := float64(r.MediaFile.FileSize) / (1024 * 1024 * 1024)
	var sizeScore float64
	
	// Optimal size ranges for different resolutions
	if r.TitleInfo.Resolution >= 2160 { // 4K
		if sizeGB >= 15 && sizeGB <= 30 {
			sizeScore = 100 // Optimal 4K size
		} else if sizeGB >= 10 && sizeGB <= 40 {
			sizeScore = 80 // Acceptable 4K size
		} else if sizeGB >= 5 && sizeGB <= 50 {
			sizeScore = 60 // Tolerable 4K size
		} else {
			sizeScore = 20 // Too small or too large
		}
	} else if r.TitleInfo.Resolution >= 1080 { // 1080p
		if sizeGB >= 4 && sizeGB <= 15 {
			sizeScore = 100 // Optimal 1080p size
		} else if sizeGB >= 2 && sizeGB <= 20 {
			sizeScore = 80 // Acceptable 1080p size
		} else if sizeGB >= 1 && sizeGB <= 25 {
			sizeScore = 60 // Tolerable 1080p size
		} else {
			sizeScore = 20 // Too small or too large
		}
	} else if r.TitleInfo.Resolution >= 720 { // 720p
		if sizeGB >= 1 && sizeGB <= 8 {
			sizeScore = 100 // Optimal 720p size
		} else if sizeGB >= 0.5 && sizeGB <= 12 {
			sizeScore = 80 // Acceptable 720p size
		} else if sizeGB >= 0.3 && sizeGB <= 15 {
			sizeScore = 60 // Tolerable 720p size
		} else {
			sizeScore = 20 // Too small or too large
		}
	} else { // 480p and below
		if sizeGB >= 0.5 && sizeGB <= 4 {
			sizeScore = 100 // Optimal 480p size
		} else if sizeGB >= 0.2 && sizeGB <= 6 {
			sizeScore = 80 // Acceptable 480p size
		} else if sizeGB >= 0.1 && sizeGB <= 8 {
			sizeScore = 60 // Tolerable 480p size
		} else {
			sizeScore = 20 // Too small or too large
		}
	}
	
	// Check if using Real Debrid (has cached files)
	usingDebrid := r.RDClient != nil && len(r.Files) > 0
	
	if usingDebrid {
		// With Real Debrid: Seeders irrelevant, focus on quality
		// Weighted combination: Visual (50%) + Source (35%) + Size (15%)
		totalScore := (resolutionScore * 0.5) + (sourceScore * 0.35) + (sizeScore * 0.15)
		return totalScore
	} else {
		// Without Real Debrid: Seeders important for speed
		seederScore := float64(r.Torrent.Seeders)
		if seederScore > 100 {
			seederScore = 100 // Cap at 100 for normalization
		}
		
		// Weighted combination: Speed (40%) + Visual (30%) + Source (20%) + Size (10%)
		totalScore := (seederScore * 0.4) + (resolutionScore * 0.3) + (sourceScore * 0.2) + (sizeScore * 0.1)
		return totalScore
	}
}

// groupByResolution groups torrents by resolution and takes the top N from each group
func groupByResolution(records []*streamRecord, maxPerGroup int) []*streamRecord {
	// Group by resolution
	groups := make(map[int][]*streamRecord)
	for _, record := range records {
		resolution := record.TitleInfo.Resolution
		groups[resolution] = append(groups[resolution], record)
	}

	// Sort each group by quality score
	for resolution, group := range groups {
		slices.SortFunc(group, func(r1, r2 *streamRecord) int {
			score1 := calculateQualityScore(r1)
			score2 := calculateQualityScore(r2)
			
			if score1 > score2 {
				return -1
			}
			if score1 < score2 {
				return 1
			}
			return 0
		})

		// Take only the top N from each group
		if len(group) > maxPerGroup {
			group = group[:maxPerGroup]
		}
		groups[resolution] = group
	}

	// Interleave groups in resolution order: 720p, 1080p, 4K, then others
	var result []*streamRecord
	resolutionOrder := []int{720, 1080, 2160, 480, 360}

	for _, resolution := range resolutionOrder {
		if group, exists := groups[resolution]; exists {
			result = append(result, group...)
		}
	}

	// Add any remaining resolutions not in the predefined order
	for resolution, group := range groups {
		found := false
		for _, orderedRes := range resolutionOrder {
			if resolution == orderedRes {
				found = true
				break
			}
		}
		if !found {
			result = append(result, group...)
		}
	}

	return result
}

func (add *Addon) parseTorrentTitle(r *streamRecord) (*streamRecord, error) {
	r.TitleInfo = titleparser.Parse(r.Torrent.Title)
	return r, nil
}

func (add *Addon) locateMediaFile(r *streamRecord) ([]*streamRecord, error) {
	// If no debrid client is available OR no files from debrid, use torrent data directly
	if r.RDClient == nil || len(r.Files) == 0 {
		r.MediaFile = &realdebrid.File{
			ID:       r.Torrent.InfoHash,
			FileName: r.Torrent.FileName,
			FileSize: uint64(r.Torrent.Size),
		}
		return []*streamRecord{r}, nil
	}

	// Original debrid-based file filtering logic
	switch r.ContentType {
	case ContentTypeMovie:
		r.MediaFile = findMovieMediaFile(r.Files)
	case ContentTypeSeries:
		// Season & Episode together
		r.MediaFile = findEpisodeMediaFile(r.Files, fmt.Sprintf(`(?i)(\b|_)S?(%d|%02d)[x\.\-]?E?%02d(\b|_)`, r.Season, r.Season, r.Episode))

		if r.MediaFile == nil {
			// Season & Episode are separate
			r.MediaFile = findEpisodeMediaFile(r.Files, fmt.Sprintf(`(?i)\bS?%02d\b.+\bE?%02d\b`, r.Season, r.Episode))
		}

		if r.MediaFile == nil {
			// Episode only
			r.MediaFile = findEpisodeMediaFile(r.Files, fmt.Sprintf(`(?i)\bE?(%d|%02d)\b`, r.Episode, r.Episode))
		}
	default:
		return nil, errors.New("invalid content type")
	}

	if r.MediaFile == nil {
		log.Infof("Couldn't locate media file: %s, %d, %d", r.Torrent.Title, r.Season, r.Episode)
		return nil, nil
	}

	if r.MediaFile.FileSize >= maxSizeInBytes || r.MediaFile.FileSize < minSizeInBytes {
		log.Debugf("Excluded %s due to file size: %s (outside range %s - %s)", 
			r.Torrent.Title, 
			bytesConvert(r.MediaFile.FileSize),
			bytesConvert(minSizeInBytes),
			bytesConvert(maxSizeInBytes))
		return nil, nil
	}

	return []*streamRecord{r}, nil
}

func deduplicateTorrent() func(r *streamRecord) bool {
	found := &sync.Map{}
	return func(r *streamRecord) bool {
		if r.Torrent.InfoHash == "" {
			log.Infof("Skipped %s due to empty hash", r.Torrent.Title)
			return false
		}

		if _, loaded := found.LoadOrStore(r.Torrent.InfoHash, struct{}{}); loaded {
			log.Infof("Skipped %s due to duplication of %s", r.Torrent.Title, r.Torrent.InfoHash)
			return false
		}

		return true
	}
}

func findEpisodeMediaFile(files []*realdebrid.File, pattern string) *realdebrid.File {
	var mediaFile *realdebrid.File
	compiled := regexp.MustCompile(pattern)
	for _, f := range files {
		if !hasMediaExtension(f.FileName) || !compiled.MatchString(f.FileName) {
			continue
		}

		if mediaFile == nil || mediaFile.FileSize < f.FileSize {
			mediaFile = f
		}
	}

	return mediaFile
}

func findMovieMediaFile(files []*realdebrid.File) *realdebrid.File {
	var mediaFile *realdebrid.File
	for _, f := range files {
		if !hasMediaExtension(f.FileName) {
			continue
		}

		if mediaFile == nil || mediaFile.FileSize < f.FileSize {
			mediaFile = f
		}
	}

	return mediaFile
}

func hasMediaExtension(fileName string) bool {
	fileName = strings.ToLower(fileName)
	for _, extension := range mediaContainerExtensions {
		if strings.HasSuffix(fileName, extension) {
			return true
		}
	}

	return false
}

func (add *Addon) createExcludeTorrentsFilter() func(r *streamRecord) bool {
	return func(r *streamRecord) bool {
		return excludeTorrents(r)
	}
}

func excludeTorrents(r *streamRecord) bool {
	// Parse user preferences with defaults
	minRes, _ := strconv.Atoi(r.UserData.MinResolution)
	maxRes, _ := strconv.Atoi(r.UserData.MaxResolution)
	minSizeGB, _ := strconv.ParseFloat(r.UserData.MinSize, 64)
	maxSizeGB, _ := strconv.ParseFloat(r.UserData.MaxSize, 64)
	minSeeders, _ := strconv.Atoi(r.UserData.MinSeeders)
	
	
	// Use defaults if not set or invalid
	if minSizeGB == 0 {
		minSizeGB = 0.1 // 100MB
	}
	if maxSizeGB == 0 {
		maxSizeGB = 30.0 // 30GB
	}
	if minSeeders == 0 {
		minSeeders = 1
	}
	
	// Convert GB to bytes
	minSizeBytes := uint64(minSizeGB * 1024 * 1024 * 1024)
	maxSizeBytes := uint64(maxSizeGB * 1024 * 1024 * 1024)
	
	// Quality filtering - use user preferences or defaults
	var excludedQualities []string
	if r.UserData.ExcludedQualities != "" {
		excludedQualities = strings.Split(strings.ToLower(r.UserData.ExcludedQualities), ",")
		for i, q := range excludedQualities {
			excludedQualities[i] = strings.TrimSpace(q)
		}
	} else {
		excludedQualities = avoidQualities
	}
	
	qualityOK := !slices.Contains(excludedQualities, r.TitleInfo.Quality) && !r.TitleInfo.ThreeD
	
	// File size filtering
	sizeOK := uint64(r.Torrent.Size) >= minSizeBytes && uint64(r.Torrent.Size) <= maxSizeBytes
	
	// Resolution filtering - use user preferences
	resolutionOK := true
	if minRes > 0 && r.TitleInfo.Resolution > 0 && r.TitleInfo.Resolution < minRes {
		resolutionOK = false
	}
	if maxRes > 0 && r.TitleInfo.Resolution > 0 && r.TitleInfo.Resolution > maxRes {
		resolutionOK = false
	}
	// If maxRes is 0 (unset), don't filter by max resolution
	
	// Content matching
	imdbOK := (r.Torrent.Imdb == 0 || r.Torrent.Imdb == r.MetaInfo.IMDBID)
	yearOK := (r.TitleInfo.Year == 0 || (r.MetaInfo.FromYear <= r.TitleInfo.Year && r.MetaInfo.ToYear >= r.TitleInfo.Year))
	seasonOK := r.ContentType != ContentTypeSeries || (r.TitleInfo.FromSeason == 0 || (r.TitleInfo.FromSeason <= r.Season && r.TitleInfo.ToSeason >= r.Season))
	episodeOK := r.ContentType != ContentTypeSeries || (r.TitleInfo.Episode == 0 || r.TitleInfo.Episode == r.Episode)
	
	// Seeders check
	seedersOK := r.Torrent.Seeders >= uint(minSeeders)
	
	torrentOK := qualityOK && sizeOK && resolutionOK && imdbOK && yearOK && seasonOK && episodeOK && seedersOK
	
	// Title similarity check for torrents without IMDB ID
	if torrentOK && r.Torrent.Imdb == 0 {
		diff := checkTitleSimilarity(r.MetaInfo.Name, r.TitleInfo.Title)
		torrentOK = torrentOK && diff < maxTitleDistance
		if !torrentOK && (diff < maxTitleDistance+3) {
			log.Infof("Excluded %s, title: %s, diff: %d", r.Torrent.Title, r.TitleInfo.Title, diff)
		}
	}

	
	return torrentOK
}

func checkTitleSimilarity(left, right string) int {
	left = nonWordCharacter.ReplaceAllString(left, "")
	right = nonWordCharacter.ReplaceAllString(right, "")
	metrics := &metrics.Levenshtein{
		CaseSensitive: false,
		InsertCost:    2,
		DeleteCost:    3,
		ReplaceCost:   3,
	}
	return metrics.Distance(left, right)
}


// getQualityScore returns a score for quality preference (higher = better)
func getQualityScore(quality string) int {
	switch quality {
	case "bdremux", "brremux":
		return 10
	case "web-dl", "webrip":
		return 9
	case "bluray":
		return 8
	case "hdrip":
		return 7
	case "dvdrip":
		return 6
	case "dvd":
		return 5
	case "tvrip":
		return 4
	case "cam", "camrip", "telesync", "tsrip":
		return 1
	default:
		return 3 // Unknown quality gets medium score
	}
}


func getIPAddress(c *fiber.Ctx) string {
	ips := c.GetReqHeaders()["Cf-Connecting-Ip"]
	if len(ips) > 0 {
		return ips[0]
	}

	return ""
}

func parseUserData(add *Addon, c *fiber.Ctx) (*UserData, error) {
	userDataRaw := c.Params("userData")
	if userDataRaw == "" {
		log.Errorf("No userData parameter provided")
		return nil, errors.New("configuration is required")
	}

	userDataJson, err := url.PathUnescape(userDataRaw)
	if err != nil {
		log.Errorf("Failed URL decode userdata %s: %v", userDataRaw, err)
		return nil, errors.New("invalid userData")
	}

	log.Infof("Parsing user data: %s", userDataJson)

	userData := &UserData{}
	err = json.Unmarshal([]byte(userDataJson), userData)
	if err != nil {
		log.Errorf("Failed JSON unmarshal userdata %s: %v", userDataJson, err)
		return nil, errors.New("invalid userData")
	}

	// Apply defaults for any missing values
	userData.ApplyDefaults()

	log.Infof("Parsed user data: ProwlarrURL=%s, ProwlarrAPIKey=%s, RDAPIKey=%s", 
		userData.ProwlarrURL, 
		userData.ProwlarrAPIKey, 
		userData.RDAPIKey)

	// Determine final configuration (environment variables + UI overrides)
	finalProwlarrURL := userData.ProwlarrURL
	finalProwlarrAPIKey := userData.ProwlarrAPIKey
	finalRealDebridAPIKey := userData.RDAPIKey
	
	// If UI values are empty, use environment variables
	if finalProwlarrURL == "" && add.prowlarrURL != "" {
		finalProwlarrURL = add.prowlarrURL
	}
	if finalProwlarrAPIKey == "" && add.prowlarrAPIKey != "" {
		finalProwlarrAPIKey = add.prowlarrAPIKey
	}
	if finalRealDebridAPIKey == "" && add.realDebridAPIKey != "" {
		finalRealDebridAPIKey = add.realDebridAPIKey
	}
	
	// Validate Prowlarr configuration consistency
	// If user provides URL or API key in UI, both must be provided (or use env for missing one)
	if (userData.ProwlarrURL != "" && userData.ProwlarrAPIKey == "") ||
	   (userData.ProwlarrURL == "" && userData.ProwlarrAPIKey != "") {
		// Check if we can fill the missing value from environment
		if userData.ProwlarrURL != "" && add.prowlarrAPIKey != "" {
			log.Infof("Using UI Prowlarr URL with environment API key")
		} else if userData.ProwlarrAPIKey != "" && add.prowlarrURL != "" {
			log.Infof("Using UI Prowlarr API key with environment URL")
		} else {
			log.Errorf("Both Prowlarr URL and API key must be provided together")
			return nil, errors.New("prowlarr URL and API key must be provided together")
		}
	}

	// Validate that at least one service is configured
	prowlarrConfigured := (finalProwlarrURL != "" && finalProwlarrAPIKey != "")
	realDebridConfigured := (finalRealDebridAPIKey != "")
	
	if !prowlarrConfigured && !realDebridConfigured {
		log.Errorf("No services configured: Prowlarr or Real Debrid required")
		return nil, errors.New("prowlarr or real debrid configuration is required")
	}
	
	log.Infof("Final configuration - Prowlarr: %v (URL: %v, APIKey: %v), Real Debrid: %v (APIKey: %v)", 
		prowlarrConfigured, finalProwlarrURL, finalProwlarrAPIKey != "",
		realDebridConfigured, finalRealDebridAPIKey != "")

	return userData, nil
}

func formatResolution(resolution int) string {
	if name, ok := resolutionName[resolution]; ok {
		return name
	}

	return fmt.Sprintf("%dp", resolution)
}

func formatStreamName(titleInfo *titleparser.MetaInfo, isCached bool) string {
	// Format: StreamX on first line, [Resolution] on second line, [Codec] on third line
	cachedIndicator := ""
	if isCached {
		cachedIndicator = " ⚡"
	}
	
	firstLine := fmt.Sprintf("StreamX%s", cachedIndicator)
	lines := []string{firstLine}
	
	// Always add resolution in brackets on second line
	resolution := formatResolution(titleInfo.Resolution)
	lines = append(lines, fmt.Sprintf("[%s]", resolution))
	
	// Add codec in brackets on third line only if available
	if titleInfo.Codec != "" {
		codec := strings.ToUpper(titleInfo.Codec)
		lines = append(lines, fmt.Sprintf("[%s]", codec))
	}
	
	return strings.Join(lines, "\n")
}

func formatStreamTitle(titleInfo *titleparser.MetaInfo, fileSize uint64, seeders uint, indexerName string, isCached bool) string {
	// Use clean parsed title instead of raw filename
	cleanTitle := titleInfo.Title
	if cleanTitle == "" {
		cleanTitle = "Unknown Title"
	} else {
		// Replace dots with spaces for better readability
		cleanTitle = strings.ReplaceAll(cleanTitle, ".", " ")
		// Clean up multiple spaces
		cleanTitle = strings.Join(strings.Fields(cleanTitle), " ")
	}
	
	// Add year if available
	if titleInfo.Year > 0 {
		cleanTitle = fmt.Sprintf("%s (%d)", cleanTitle, titleInfo.Year)
	}
	
	// Add season/episode info if available
	if titleInfo.FromSeason > 0 && titleInfo.Episode > 0 {
		cleanTitle = fmt.Sprintf("%s S%02dE%02d", cleanTitle, titleInfo.FromSeason, titleInfo.Episode)
	} else if titleInfo.FromSeason > 0 {
		if titleInfo.ToSeason > titleInfo.FromSeason {
			cleanTitle = fmt.Sprintf("%s S%02d-S%02d", cleanTitle, titleInfo.FromSeason, titleInfo.ToSeason)
		} else {
			cleanTitle = fmt.Sprintf("%s S%02d", cleanTitle, titleInfo.FromSeason)
		}
	}
	
	// Format with emojis: Seeders | Size | Quality
	info := fmt.Sprintf("👤 %d | 💾 %s", 
		seeders,
		bytesConvert(fileSize))
	
	// Add quality after size if available
	if titleInfo.Quality != "" {
		quality := strings.ToUpper(titleInfo.Quality)
		quality = strings.ReplaceAll(quality, "-", " ")
		quality = strings.ReplaceAll(quality, "_", " ")
		info = fmt.Sprintf("%s | [%s]", info, quality)
	}
	
	// Add provider as third line
	provider := fmt.Sprintf("🔍 %s", indexerName)
	
	// Add language as fourth line if available
	lines := []string{cleanTitle, info, provider}
	if titleInfo.Language != "" {
		language := strings.ToUpper(titleInfo.Language)
		lines = append(lines, fmt.Sprintf("🌍 %s", language))
	}
	
	return strings.Join(lines, "\n")
}

