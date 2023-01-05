# syntax=docker/dockerfile:1
# latest golang version
FROM golang:latest

LABEL maintainer="Yannick Rast" \
      name="webprogrammierung" \
      version="0.1"

WORKDIR /app

# Download necessary Go modules
COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /webprogrammierung

EXPOSE 9090

CMD [ "/webprogrammierung" ]