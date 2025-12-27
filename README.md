# telegram-upload-watcher
Monitor and send organized folder contents to Telegram channel, group or user/监控文件夹整理发送到telegram的channel 或者群组或者用户

## Setup / 安装 (uv)
```bash
uv sync
```

## Config / 配置 (INI)
```ini
[Telegram]
api_url = https://api.telegram.org

[Token1]
name = default
id = main
token = 123456:ABCDEF
```
Example file: `config.example.ini` / 示例文件：`config.example.ini`

## CLI / 命令行
Send a text message / 发送文本消息:
```bash
telegram-send send-message \
  --chat-id "-1001234567890" \
  --message "Hello from telegram-upload-watcher" \
  --bot-token "123456:ABCDEF"
```

Send images from a directory / 发送目录图片:
```bash
telegram-send send-images \
  --chat-id "-1001234567890" \
  --image-dir /path/to/images \
  --config /path/to/config.ini
```

Send images from a zip / 发送压缩包图片:
```bash
telegram-send send-images \
  --chat-id "-1001234567890" \
  --zip-file /path/to/images.zip \
  --config /path/to/config.ini
```

## Go CLI / Go 命令行
Build / 编译:
```bash
go build -o telegram-send-go ./go/cmd/telegram-send-go
```

Send a text message / 发送文本消息:
```bash
./telegram-send-go send-message \
  --chat-id "-1001234567890" \
  --message "Hello from Go" \
  --config ./config.example.ini
```

Watch folder / 监控文件夹:
```bash
./telegram-send-go watch \
  --watch-dir /path/to/watch \
  --chat-id "-1001234567890" \
  --config ./config.example.ini \
  --notify
```

## Build Tools / 构建工具
Just:
```bash
just build
```

Make:
```bash
make build
```

GoReleaser (snapshot):
```bash
goreleaser release --snapshot --clean
```

## Watch / 监控
Watch a folder and send queued images / 监控文件夹并按队列发送图片:
```bash
telegram-send watch \
  --watch-dir /path/to/watch \
  --chat-id "-1001234567890" \
  --config /path/to/config.ini
```

Useful options / 常用参数:
- `--recursive` enable recursive scan / 递归扫描
- `--exclude "*.tmp"` glob excludes (repeatable) / 排除规则 (可重复)
- `--scan-interval 30` scan interval seconds / 扫描间隔秒
- `--send-interval 30` send interval seconds / 发送间隔秒
- `--queue-file queue.jsonl` queue persistence file / 队列持久化文件
- `--settle-seconds 5` wait for file stability / 文件稳定等待
- `--pause-every 100` pause after N images / 每发送 N 张暂停
- `--pause-seconds 60` pause duration / 暂停时长
- `--max-dimension 2000` max image dimension / 最大边
- `--max-bytes 5242880` max image bytes before PNG compress / 超过则 PNG 压缩
- `--png-start-level 8` PNG compress start level / PNG 压缩起始等级
- `--notify` enable watch notifications / 开启监控通知
- `--notify-interval 300` status interval seconds / 状态通知间隔秒
