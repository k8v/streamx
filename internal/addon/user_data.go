package addon

type UserData struct {
	RDAPIKey          string `json:"rd"`
	ProwlarrURL       string `json:"pUrl"`
	ProwlarrAPIKey    string `json:"pKey"`
	MinResolution     string `json:"minRes"`
	MaxResolution     string `json:"maxRes"`
	MinSize           string `json:"minSize"`
	MaxSize           string `json:"maxSize"`
	MinSeeders        string `json:"minSeeders"`
	ExcludedQualities string `json:"excludedQualities"`
	SearchTimeout     string `json:"searchTimeout"`
	SortMethod        string `json:"sortMethod"`
}

// NewUserDataWithDefaults creates UserData with sensible defaults
func NewUserDataWithDefaults() *UserData {
	return &UserData{
		MinResolution:     "720",      // 720p minimum for good quality
		MaxResolution:     "2160",     // 4K maximum
		MinSize:           "0.5",      // 0.5GB minimum to avoid low quality
		MaxSize:           "25",       // 25GB maximum to avoid oversized files
		MinSeeders:        "10",        // 5 seeders minimum for reliability
		ExcludedQualities: "cam,camrip,telesync,tsrip,hdcam,tc,ppvrip,r5,vhsscr", // Exclude poor quality
		SearchTimeout:     "60",       // 45 seconds for good balance
		SortMethod:        "quality",  // Quality score method
	}
}

// ApplyDefaults fills in any missing values with defaults
func (u *UserData) ApplyDefaults() {
	defaults := NewUserDataWithDefaults()
	
	if u.MinResolution == "" {
		u.MinResolution = defaults.MinResolution
	}
	if u.MaxResolution == "" {
		u.MaxResolution = defaults.MaxResolution
	}
	if u.MinSize == "" {
		u.MinSize = defaults.MinSize
	}
	if u.MaxSize == "" {
		u.MaxSize = defaults.MaxSize
	}
	if u.MinSeeders == "" {
		u.MinSeeders = defaults.MinSeeders
	}
	if u.ExcludedQualities == "" {
		u.ExcludedQualities = defaults.ExcludedQualities
	}
	if u.SearchTimeout == "" {
		u.SearchTimeout = defaults.SearchTimeout
	}
	if u.SortMethod == "" {
		u.SortMethod = defaults.SortMethod
	}
}
