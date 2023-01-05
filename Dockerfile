# syntax=docker/dockerfile:1
# latest golang version
FROM golang:latest

WORKDIR /app

# Download necessary Go modules
COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /webprogrammierung

EXPOSE 9090

CMD [ "/webprogrammierung" ]