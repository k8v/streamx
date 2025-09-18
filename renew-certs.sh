#!/bin/sh

# SSL Certificate Renewal Script for StreamX
# Checks certificate expiration and renews when needed

CERT_FILE="/etc/ssl/local-ip-co/server.pem"
RENEW_DAYS=30  # Renew if expiring within 30 days

echo "$(date): Checking SSL certificate status..."

# Check if certificate exists
if [ ! -f "$CERT_FILE" ]; then
    echo "$(date): Certificate not found, forcing renewal..."
    FORCE_RENEW=1
else
    # Check certificate expiration
    if command -v openssl >/dev/null 2>&1; then
        # Get expiration date in seconds since epoch
        CERT_END=$(openssl x509 -enddate -noout -in "$CERT_FILE" | cut -d= -f2)
        CERT_END_EPOCH=$(date -d "$CERT_END" +%s 2>/dev/null || date -j -f "%b %d %T %Y %Z" "$CERT_END" +%s 2>/dev/null)
        
        # Get current date + renewal threshold
        RENEW_EPOCH=$(date -d "+${RENEW_DAYS} days" +%s 2>/dev/null || date -j -v+${RENEW_DAYS}d +%s 2>/dev/null)
        
        if [ "$CERT_END_EPOCH" -lt "$RENEW_EPOCH" ]; then
            echo "$(date): Certificate expires within $RENEW_DAYS days, renewing..."
            FORCE_RENEW=1
        else
            echo "$(date): Certificate is valid, no renewal needed"
            FORCE_RENEW=0
        fi
    else
        echo "$(date): OpenSSL not available, forcing renewal to be safe..."
        FORCE_RENEW=1
    fi
fi

# Only renew if needed
if [ "$FORCE_RENEW" = "1" ]; then
    echo "$(date): Downloading fresh certificates..."
    
    # Download new certificates
    wget -O /etc/ssl/local-ip-co/server.pem.new http://local-ip.co/cert/server.pem
    wget -O /etc/ssl/local-ip-co/server.key.new http://local-ip.co/cert/server.key  
    wget -O /etc/ssl/local-ip-co/chain.pem.new http://local-ip.co/cert/chain.pem

    # Verify download was successful
    if [ -s /etc/ssl/local-ip-co/server.pem.new ] && [ -s /etc/ssl/local-ip-co/server.key.new ]; then
        # Replace old certificates with new ones
        mv /etc/ssl/local-ip-co/server.pem.new /etc/ssl/local-ip-co/server.pem
        mv /etc/ssl/local-ip-co/server.key.new /etc/ssl/local-ip-co/server.key
        mv /etc/ssl/local-ip-co/chain.pem.new /etc/ssl/local-ip-co/chain.pem
        
        echo "$(date): SSL certificates renewed successfully"
    else
        echo "$(date): Failed to download new certificates - keeping existing ones"
        rm -f /etc/ssl/local-ip-co/*.new
    fi
else
    echo "$(date): Certificate check complete - no action needed"
fi
