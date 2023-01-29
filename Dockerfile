# syntax=docker/dockerfile:1
# latest golang version
FROM golang:latest AS build

LABEL maintainer="Yannick Rast" \
      name="webprogrammierung" \
      version="0.1"

WORKDIR /webprogrammierung

COPY ./main.go .
RUN mkdir -p vendor
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go mod vendor
RUN go build -o Webprogrammierung main.go

FROM debian
WORKDIR /app
COPY --from=build /webprogrammierung/Webprogrammierung .
COPY files/ files/
COPY static/ static/
COPY templates/ templates/

EXPOSE 9090
CMD ["/app/Webprogrammierung"]