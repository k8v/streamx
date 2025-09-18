Étape de construction
FROM golang:1.22-alpine

WORKDIR /src

Installer les dépendances nécessaires pour la compilation et l'exécution
RUN apk add --no-cache wget curl openssl dcron

COPY go.* .
RUN go mod download

COPY . .

Construction de l'exécutable
RUN go build -o /bin/server -ldflags="-X 'main.version=$(cat VERSION.txt)'" ./cmd/server/main.go

Configuration de l'environnement de production
Téléchargement des certificats
RUN mkdir -p /etc/ssl/local-ip-co
RUN wget -O /etc/ssl/local-ip-co/server.pem http://local-ip.co/cert/server.pem
RUN wget -O /etc/ssl/local-ip-co/server.key http://local-ip.co/cert/server.key
RUN wget -O /etc/ssl/local-ip-co/chain.pem http://local-ip.co/cert/chain.pem

Copier et configurer le script de renouvellement des certificats
COPY renew-certs.sh /usr/local/bin/renew-certs.sh
RUN chmod +x /usr/local/bin/renew-certs.sh

Configurer la tâche cron pour le renouvellement des certificats
RUN echo '0 2 * * 0 /usr/local/bin/renew-certs.sh >> /var/log/cert-renewal.log 2>&1' > /etc/crontabs/root

Copier les fichiers statiques de l'application
COPY ./internal/static/logo.svg /bin/logo.svg

Copier le script de démarrage
COPY start-streamx.sh /bin/start-streamx.sh
RUN chmod +x /bin/start-streamx.sh

Exposer les ports HTTP et HTTPS
EXPOSE 7000 7443

Commande de démarrage
CMD ["/bin/start-streamx.sh"]
