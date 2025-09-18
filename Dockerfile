FROM golang:1.22-alpine AS build
WORKDIR /src

# Install wget for downloading certificates
RUN apk add --no-cache wget

COPY go.* .
RUN go mod download

COPY . .
RUN go build -o /bin/server -ldflags="-X 'main.version=$(cat VERSION.txt)'" /src/cmd/server/main.go

FROM alpine

# Install runtime dependencies including cron and openssl for cert validation
RUN apk add --no-cache ca-certificates wget curl dcron openssl

# Download local-ip.co SSL certificates
RUN mkdir -p /etc/ssl/local-ip-co
RUN wget -O /etc/ssl/local-ip-co/server.pem http://local-ip.co/cert/server.pem
RUN wget -O /etc/ssl/local-ip-co/server.key http://local-ip.co/cert/server.key
RUN wget -O /etc/ssl/local-ip-co/chain.pem http://local-ip.co/cert/chain.pem

# Copy and setup certificate renewal script
COPY renew-certs.sh /usr/local/bin/renew-certs.sh
RUN chmod +x /usr/local/bin/renew-certs.sh

# Set up cron job to renew certificates weekly (every Sunday at 2 AM)
RUN echo '0 2 * * 0 /usr/local/bin/renew-certs.sh >> /var/log/cert-renewal.log 2>&1' > /etc/crontabs/root

# Copy application files
COPY --from=build /bin/server /bin/server
COPY --from=build /src/internal/static/logo.svg /bin/logo.svg

# Copy startup script
COPY start-streamx.sh /bin/start-streamx.sh
RUN chmod +x /bin/start-streamx.sh

# Expose both HTTP and HTTPS ports
EXPOSE 7000 7443

CMD ["/bin/start-streamx.sh"]
