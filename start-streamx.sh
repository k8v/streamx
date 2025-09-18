#!/bin/sh

# StreamX Startup Script
echo "=================================="
echo "StreamX Starting..."
echo "=================================="

# Check if SSL is enabled (default: true)
SSL_ENABLED=${SSL_ENABLED:-true}

if [ "$SSL_ENABLED" = "true" ]; then
    echo "SSL Mode: ENABLED"
    
    # Start cron daemon for certificate renewal
    crond -b
    echo "Certificate auto-renewal enabled (weekly, smart expiration checking)"
    
    # Get host IP from environment variable (required for SSL)
    if [ -z "$HOST_IP" ]; then
        echo "❌ Error: HOST_IP environment variable not set"
        echo "Please set your local IP address for SSL:"
        echo "Example: HOST_IP=192.168.1.41 docker compose up -d"
        exit 1
    fi
    
    # Format IP for local-ip.co (replace dots with dashes)
    FORMATTED_IP=$(echo $HOST_IP | tr "." "-")
    SSL_DOMAIN="${FORMATTED_IP}.my.local-ip.co"
    
    echo "Host IP: $HOST_IP"
    echo "SSL Domain: $SSL_DOMAIN"
    echo "=================================="
    echo "StreamX SSL Ready!"
    echo "=================================="
    echo "HTTP Config: http://localhost:7000"
    echo "HTTPS Stremio: https://${SSL_DOMAIN}"
    echo "=================================="
else
    echo "SSL Mode: DISABLED"
    echo "=================================="
    echo "StreamX HTTP-Only Ready!"
    echo "=================================="
    echo "HTTP Config: http://localhost:7000"
    echo "Note: Use a tunnel service for Stremio HTTPS requirement"
    echo "=================================="
fi

# Start the StreamX server
exec /bin/server
