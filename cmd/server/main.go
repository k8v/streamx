package main

import (
	"os"
	"regexp"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	_ "github.com/joho/godotenv/autoload"

	"github.com/dbytex91/streamx/internal/addon"
	"github.com/dbytex91/streamx/internal/static"
)

type config struct {
	ProwlarrURL    string `env:"PROWLARR_URL"`
	ProwlarrAPIKey string `env:"PROWLARR_API_KEY"`
	RealDebridKey  string `env:"REAL_DEBRID_API_KEY"`
}

var (
	maskedPathPattern = regexp.MustCompile(`^/([\w%]+)/(?:configure|stream|download|manifest)`)
	version           = "2.0.0"
)

func main() {
	cfg := config{}
	_ = env.Parse(&cfg)

	app := fiber.New()
	app.Use(cors.New())
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(logger.New(logger.Config{
		CustomTags: map[string]logger.LogFunc{
			"maskedPath": func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				urlPath := c.Path()
				loc := maskedPathPattern.FindStringSubmatchIndex(urlPath)
				if len(loc) > 3 {
					return output.WriteString(urlPath[:loc[2]] + "***" + urlPath[loc[3]:])
				} else {
					return output.WriteString(urlPath)
				}
			},
		},
		Format:        "${time} | ${status} | ${latency} | ${ip} | ${method} | ${maskedPath} | ${error}\n",
		TimeFormat:    "15:04:05",
		TimeZone:      "Local",
		TimeInterval:  500 * time.Millisecond,
		Output:        os.Stdout,
		DisableColors: false,
	}))

	opts := []addon.Option{
		addon.WithID("com.streamx.addon"),
		addon.WithName("StreamX"),
		addon.WithVersion(version),
	}
	
	// Only add Prowlarr client if both URL and API key are provided
	if cfg.ProwlarrURL != "" && cfg.ProwlarrAPIKey != "" {
		opts = append(opts, addon.WithProwlarr(cfg.ProwlarrURL, cfg.ProwlarrAPIKey))
	}
	
	// Only add Real Debrid client if API key is provided
	if cfg.RealDebridKey != "" {
		opts = append(opts, addon.WithRealDebrid(cfg.RealDebridKey))
	}
	
	add := addon.New(opts...)

	app.Get("/manifest.json", add.HandleGetManifest)
	app.Get("/:userData/manifest.json", add.HandleGetManifest)
	app.Get("/logo", add.HandleLogo)
	app.Get("/:userData/logo", add.HandleLogo)
	app.Get("/stream/:type/:id.json", add.HandleGetStreams)
	app.Get("/:userData/stream/:type/:id.json", add.HandleGetStreams)
	app.Get("/download/:infoHash/:fileID", add.HandleDownload)
	app.Get("/:userData/download/:infoHash/:fileID", add.HandleDownload)
	app.Head("/download/:infoHash/:fileID", add.HandleDownload)
	app.Head("/:userData/download/:infoHash/:fileID", add.HandleDownload)
	app.Get("/configure", static.HandleConfigure)
	app.Get("/:userData/configure", static.HandleConfigure)

	// Check if SSL is enabled
	sslEnabled := os.Getenv("SSL_ENABLED") == "true"
	
	if sslEnabled {
		// Start HTTPS server in a goroutine
		go func() {
			httpsApp := fiber.New(fiber.Config{
				AppName: "StreamX SSL",
			})
			
			// Add same middleware as HTTP server
			httpsApp.Use(cors.New())
			httpsApp.Use(recover.New(recover.Config{
				EnableStackTrace: true,
			}))
			
			// Copy all routes to HTTPS app
			httpsApp.Get("/manifest.json", add.HandleGetManifest)
			httpsApp.Get("/:userData/manifest.json", add.HandleGetManifest)
			httpsApp.Get("/logo", add.HandleLogo)
			httpsApp.Get("/:userData/logo", add.HandleLogo)
			httpsApp.Get("/stream/:type/:id.json", add.HandleGetStreams)
			httpsApp.Get("/:userData/stream/:type/:id.json", add.HandleGetStreams)
			httpsApp.Get("/download/:infoHash/:fileID", add.HandleDownload)
			httpsApp.Get("/:userData/download/:infoHash/:fileID", add.HandleDownload)
			httpsApp.Head("/download/:infoHash/:fileID", add.HandleDownload)
			httpsApp.Head("/:userData/download/:infoHash/:fileID", add.HandleDownload)
			httpsApp.Get("/configure", static.HandleConfigure)
			httpsApp.Get("/:userData/configure", static.HandleConfigure)
			
			// SSL certificate paths
			certFile := "/etc/ssl/local-ip-co/server.pem"
			keyFile := "/etc/ssl/local-ip-co/server.key"
			
			log.Infof("Starting HTTPS server on :7443 with SSL domain: %s", os.Getenv("SSL_DOMAIN"))
			log.Fatal(httpsApp.ListenTLS(":7443", certFile, keyFile))
		}()
	}
	
	log.Infof("Starting HTTP server on :7000")
	log.Fatal(app.Listen(":7000"))
}
