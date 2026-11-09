FROM golang:alpine AS build

RUN apk add --no-cache --update gcc g++

COPY . /app
WORKDIR /app
ENV CGO_ENABLED=1
RUN go build -ldflags "-s -w" -o tracker .

FROM alpine AS run

# all configuration comes from /app/tracker_config.yaml (mount it, see
# tracker_config.docker.yaml); the sqlite db lives in /data (mount it too)
COPY --from=build /app/tracker /app/tracker

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME /data
EXPOSE 8080

CMD [ "/entrypoint.sh" ]
