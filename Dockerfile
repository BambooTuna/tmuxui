FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/tmuxui .

FROM alpine:edge
# tmux 3.5〜3.6.x はformat文字列内のtabを`_`にサニタイズするためlist-sessions等の
# タブ区切りパースが壊れる。tmux 3.7以降で修正されているためedge(3.7b)を採用する。
# LANG/LC_ALLを設定しないと3.7b含めどのバージョンでもtabがサニタイズされる。
RUN apk add --no-cache tmux git ca-certificates
# selfupdate.ApplyがバイナリをRenameで置換するため、実行ファイルを非rootユーザが
# 書けるディレクトリに配置する。
RUN mkdir -p /home/tmuxui/bin /home/tmuxui/.config/tmuxui && chown -R 1000:1000 /home/tmuxui
COPY --from=builder --chown=1000:1000 /out/tmuxui /home/tmuxui/bin/tmuxui
ENV HOME=/home/tmuxui LANG=C.UTF-8 LC_ALL=C.UTF-8 PATH=/home/tmuxui/bin:/usr/local/bin:/usr/bin:/bin
USER 1000:1000
WORKDIR /home/tmuxui
EXPOSE 6062
ENTRYPOINT ["/home/tmuxui/bin/tmuxui"]
CMD ["--host", "0.0.0.0"]
