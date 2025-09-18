# streamx (for arm64)
Source > https://gitlab.com/dx616b/streamx

An advanced torrent streaming addon with **built-in SSL support** for easy Stremio integration. Features smart filtering, quality scoring, and debrid-aware optimization. Built with Go and designed for seamless integration with Stremio.

**New in v2.0**: Built-in SSL support + intelligent defaults - no tunnel setup required!

## 🚀 Features

- **Smart Quality Scoring**: Weighted algorithm prioritizing speed (seeders) and quality (resolution, source, file size)
- **Configurable Sorting**: Choose between Quality Score optimization or Resolution Diversity
- **Advanced Filtering**: Filter by resolution, file size, seeders, and excluded qualities
- **Optional Real Debrid**: Works with or without Real Debrid integration
- **Native Torrent Streaming**: Direct streaming to Stremio when debrid is unavailable
- **Configurable Timeouts**: Adjustable search timeout (10-120 seconds)
- **Prowlarr Integration**: Search across multiple indexers simultaneously
- **Built-in SSL Support**: Direct HTTPS for Stremio (no tunnel required)
- **Docker Support**: Easy deployment with Docker Compose

## 📋 Prerequisites

### Required Software
- **Docker** and **Docker Compose** - For containerized deployment
- **Prowlarr** - Torrent indexer management (self-hosted or cloud instance)
- **Stremio** - Media streaming client

### Required Services
- **Prowlarr Instance** - Configured with torrent indexers
- **Real Debrid Account** (Optional) - For cached torrent streaming

## 🛠️ Installation

### 1. Download StreamX
```bash
git clone https://github.com/dx616b/streamx.git
cd streamx
```

### 2. Start Prowlarr (Required First Step)
```bash
# Start Prowlarr to get your API key
docker compose up -d prowlarr

# Wait for Prowlarr to start, then open: http://localhost:9696
```

**Configure Prowlarr:**
1. **Add Indexers**: Go to Indexers → Add Indexer → Choose your preferred torrent sites
2. **Get API Key**: Go to Settings → General → Security → Copy the API Key
3. **Note the API Key** - you'll need it for the next step

### 3. Configure Environment
Copy the example environment file and edit your settings:
```bash
cp env.example .env
```

**Required changes in `.env`:**
- Set `HOST_IP=your-ip-address` (find with `ipconfig` or `ifconfig`)
- Set `PROWLARR_API_KEY=your-actual-api-key` (from step 2)
- Optionally set `REAL_DEBRID_API_KEY=your-rd-key`

*SSL is enabled by default - no additional SSL configuration needed.*

### 4. Start StreamX
```bash
# Now start StreamX with your configuration
docker compose up -d

# View logs
docker compose logs -f streamx
```

### 5. Add to Stremio

**Option A: Using Environment Variables (Recommended)**
1. **Copy HTTPS URL**: `https://your-ip-address.my.local-ip.co/manifest.json` (replace with your IP)
2. **Open Stremio** → Addons → Community Addons  
3. **Paste URL** and install

**Option B: Using Web Configuration**
1. **Configure first**: Open `https://your-ip-address.my.local-ip.co/configure` in your browser
2. **Fill in settings**: Prowlarr URL, API key, optional Real Debrid key
3. **Copy generated URL**: Use the manifest URL provided after configuration
4. **Open Stremio** → Addons → Community Addons
5. **Paste URL** and install

*Note: If your `.env` file has valid API keys, Option A works immediately. If using placeholder values, use Option B to configure via web interface.*

## 🔧 HTTP-Only Version (Advanced Users)

If you prefer to disable SSL:

### Disable SSL
Add to your `.env` file:
```env
SSL_ENABLED=false
```

StreamX will run HTTP-only on port 7000. You'll need to handle HTTPS requirements for Stremio yourself (tunnel services, reverse proxy, etc.).



### 4. Build from Source (Alternative)
If you prefer to build from source instead of using the Docker Hub image:
```bash
# Use the build-specific compose file
docker compose -f docker compose.build.yml up -d

# View logs
docker compose logs -f streamx
```

### 5. Alternative: Build from Source (Go)
If you prefer to build and run locally without Docker:

```bash
# Install Go 1.22 or later
go version

# Download dependencies
go mod download

# Build the application
go build -o bin/server cmd/server/main.go

# Run the application
./bin/server
```

## ⚙️ Advanced Configuration

For advanced users who want to customize filtering and performance settings beyond the optimized defaults, use the web interface at `https://your-ip.my.local-ip.co/configure`.

### Advanced Configuration Options

#### Filtering Options
- **Min/Max Resolution**: Set resolution range (480p, 720p, 1080p, 4K)
- **Min/Max File Size**: Filter by file size in GB
- **Min Seeders**: Minimum number of seeders required
- **Excluded Qualities**: Comma-separated list of qualities to exclude (e.g., "cam,ts,scr")

#### Performance Options
- **Search Timeout**: How long to wait for indexers (10-120 seconds)
- **Sort Method**: 
  - **Quality Score**: Optimal speed + quality balance (recommended)
  - **Resolution Diversity**: Mix of 720p/1080p/4K results

### Quality Score Algorithm

StreamX uses an intelligent scoring system to rank torrents for optimal streaming experience. The algorithm adapts based on whether you're using Real Debrid or native torrent streaming.

#### Result Limiting
- **Maximum Results**: StreamX returns exactly **5 torrents** to prevent UI overload
- **Smart Filtering**: Automatically removes duplicates, bad quality, and mismatched content
- **Two Sorting Methods**: Choose between overall quality optimization or resolution diversity

#### Scoring Without Real Debrid (Native Torrent Streaming)
When using direct torrent streaming, download speed is crucial:

| Factor | Weight | Score Range | Example |
|--------|--------|-------------|---------|
| **Seeders** | 40% | 0-100 | 100+ seeders = 100 points |
| **Resolution** | 30% | 0-100 | 720p=33, 1080p=50, 4K=100 |
| **Source Quality** | 20% | 10-100 | Web-DL=100, BluRay=80, CAM=10 |
| **File Size** | 10% | 0-100 | Optimal size ranges per resolution |

**Example Calculation:**
```
1080p Web-DL with 150 seeders, 8GB file:
- Seeders: 100 points (capped) × 0.4 = 40
- Resolution: 50 points × 0.3 = 15  
- Source: 100 points × 0.2 = 20
- Size: 16 points × 0.1 = 1.6
Total Score: 76.6 points
```

#### Scoring With Real Debrid (Cached Downloads)
When using Real Debrid, seeders become irrelevant since downloads are instant:

| Factor | Weight | Score Range | Example |
|--------|--------|-------------|---------|
| **Resolution** | 50% | 0-100 | 720p=33, 1080p=50, 4K=100 |
| **Source Quality** | 35% | 10-100 | Web-DL=100, BluRay=80, CAM=10 |
| **File Size** | 15% | 0-100 | Optimal size ranges per resolution |

**Example Calculation:**
```
4K BluRay with Real Debrid, 25GB file:
- Resolution: 100 points × 0.5 = 50
- Source: 80 points × 0.35 = 28
- Size: 50 points × 0.15 = 7.5
Total Score: 85.5 points
```

#### Source Quality Scoring
Different encoding sources receive different quality scores:

| Quality | Score | Description |
|---------|-------|-------------|
| **Web-DL** | 100 | Direct web rip, excellent quality |
| **BluRay** | 80 | High-quality disc rip |
| **HDTV** | 60 | TV broadcast recording |
| **WEBRip** | 70 | Web-based rip |
| **HDCAM** | 30 | Camcorder recording |
| **CAM** | 10 | Camera recording, lowest quality |

#### Resolution Scoring Formula
```
Resolution Score = (Resolution in pixels) ÷ 21.6
```
- 720p (1280×720): 33 points
- 1080p (1920×1080): 50 points  
- 1440p (2560×1440): 67 points
- 4K (3840×2160): 100 points

#### File Size Scoring System
File size scoring uses optimal size ranges per resolution instead of "bigger is better":

| Resolution | Optimal Range | Acceptable Range | Score |
|------------|---------------|------------------|-------|
| **4K** | 15-30GB | 10-40GB | 100 points |
| **1080p** | 4-15GB | 2-20GB | 100 points |
| **720p** | 1-8GB | 0.5-12GB | 100 points |
| **480p** | 0.5-4GB | 0.2-6GB | 100 points |

Files outside acceptable ranges receive significantly lower scores (20-60 points) to prioritize practical file sizes.

#### Sorting Methods

**1. Quality Score Method (Recommended)**
- Sorts all torrents by total weighted score
- Returns top 5 overall best torrents
- Best for: Optimal speed + quality balance

**2. Resolution Diversity Method**
- Groups torrents by resolution (720p, 1080p, 4K)
- Takes best 3 from each group
- Interleaves results: 720p → 1080p → 4K → others
- Best for: Variety of quality options

#### Why Only 5 Results?
- **UI Performance**: Prevents Stremio from becoming sluggish
- **Decision Fatigue**: Too many options can be overwhelming
- **Smart Selection**: Algorithm ensures the 5 best options are presented
- **Stremio Compatibility**: Optimized for Stremio's interface design

## 🔗 Stremio Integration

### Adding the Addon

**Method 1: Direct Installation (Environment Variables)**
1. Open Stremio
2. Go to **Addons** → **Add addon**
3. Enter the addon URL: `https://your-ip-address.my.local-ip.co/manifest.json`
4. Click **Install**

*This works when your `.env` file has valid Prowlarr API keys.*

**Method 2: Web Configuration First**
1. Open browser: `https://your-ip-address.my.local-ip.co/configure`
2. Configure Prowlarr URL, API key, and optional Real Debrid
3. Copy the generated manifest URL from the configuration page
4. Open Stremio → **Addons** → **Add addon**
5. Paste the URL and click **Install**

*Use this method if you prefer web configuration or have placeholder values in `.env`.*

### Using the Addon
1. Browse your library in Stremio
2. Click on any movie or TV show
3. The addon will automatically search for torrents
4. Select your preferred quality and stream directly

## 🏗️ Architecture

### Components
- **Go Backend**: Handles torrent searching and filtering
- **Prowlarr Integration**: Searches across multiple indexers
- **Real Debrid Client**: Optional cached torrent downloads
- **Docker Container**: Isolated deployment environment

### Data Flow
1. Stremio requests streams for content
2. Addon searches Prowlarr indexers
3. Filters and sorts results by quality score
4. Returns stream URLs or torrent info hashes
5. Stremio plays content directly

## 📊 Performance

### Default Settings (Optimized for Best Experience)
StreamX now includes intelligent defaults that work great out of the box:

- **Min Resolution**: 720p (avoids low-quality releases)
- **Max Resolution**: 4K (includes all high-quality options)
- **Min File Size**: 0.5GB (filters out very low quality)
- **Max File Size**: 25GB (avoids oversized files)
- **Min Seeders**: 5 (ensures reliable download speeds)
- **Search Timeout**: 45 seconds (balance between completeness and speed)
- **Sort Method**: Quality Score (optimal speed + quality balance)
- **Excluded Qualities**: cam, camrip, telesync, etc. (blocks poor quality sources)

These defaults provide excellent streaming quality while maintaining good performance. You can customize them via the web interface if needed.

### Indexer Performance
- **Fast Indexers**: YTS, RARBG (1-2 seconds)
- **Medium Indexers**: 1337x, The Pirate Bay (3-5 seconds)
- **Slow Indexers**: Private trackers (5-10 seconds)

## 🐳 Docker Deployment

### Using Docker Hub Image (Recommended)
```yaml
services:
  streamx:
    image: dx616b/streamx:latest
    ports:
      - "7000:7000"
    environment:
      - PROWLARR_URL=${PROWLARR_URL}
      - PROWLARR_API_KEY=${PROWLARR_API_KEY}
      - RD_API_KEY=${RD_API_KEY}
    restart: unless-stopped
```

### Building from Source
```yaml
services:
  streamx:
    build: .
    ports:
      - "7000:7000"
    environment:
      - PROWLARR_URL=${PROWLARR_URL}
      - PROWLARR_API_KEY=${PROWLARR_API_KEY}
      - RD_API_KEY=${RD_API_KEY}
    restart: unless-stopped
```

### Docker Run
```bash
docker run -d \
  --name streamx \
  -p 7000:7000 \
  -e PROWLARR_URL=http://your-prowlarr:9696 \
  -e PROWLARR_API_KEY=your-key \
  -e RD_API_KEY=your-rd-key \
  dx616b/streamx:latest
```

## 🔧 Troubleshooting

### Common Issues

#### "Configure" Loop in Stremio
- Ensure Prowlarr URL and API key are correct
- Check that Prowlarr is accessible from the addon server
- Verify API key has proper permissions

#### No Results Found
- Check Prowlarr indexer status
- Verify search timeout isn't too short
- Ensure indexers are returning results for your content

#### Slow Performance
- Reduce search timeout to 30-45 seconds
- Use Quality Score sorting method
- Increase min seeders to filter out slow torrents

#### Real Debrid Issues
- Verify API key is valid and active
- Check Real Debrid service status
- Ensure torrents are cached (green checkmark)

### Logs and Debugging
```bash
# View application logs
docker compose logs -f streamx

# Check Prowlarr connectivity
curl -H "X-Api-Key: your-key" http://your-prowlarr:9696/api/v1/system/status

# Test Real Debrid API
curl -H "Authorization: Bearer your-token" https://api.real-debrid.com/rest/1.0/user
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Development Setup
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Original Prowlarr-Stremio addon by [XCraftCoder](https://github.com/XCraftCoder/stremio-prowlarr)
- SSL certificates provided by [local-ip.co](https://local-ip.co/) for seamless HTTPS integration
- Built with Go and Fiber framework

## 📞 Support

- **Issues**: [GitHub Issues](https://github.com/dx616b/streamx/issues)
- **Discussions**: [GitHub Discussions](https://github.com/dx616b/streamx/discussions)

---

**Note**: This addon is for educational and personal use only. Please respect copyright laws and terms of service of indexers and streaming platforms.
