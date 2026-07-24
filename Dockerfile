FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/tmuxui .

FROM alpine:3.20
RUN apk add --no-cache tmux git ca-certificates
COPY --from=builder /out/tmuxui /usr/local/bin/tmuxui
ENV TMUXUI_AUTOUPDATE=0 HOME=/home/tmuxui
RUN mkdir -p /home/tmuxui/.config/tmuxui && chmod 777 /home/tmuxui
USER 1000:1000
WORKDIR /home/tmuxui
EXPOSE 6062
ENTRYPOINT ["/usr/local/bin/tmuxui"]
CMD ["--host", "0.0.0.0"]
