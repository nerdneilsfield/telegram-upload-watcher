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
