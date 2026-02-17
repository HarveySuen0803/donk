<div align="center">
    <p>
        <img src="https://harvey-image.oss-cn-hangzhou.aliyuncs.com/telegram.png" alt="logo" width="200" height="auto"/>
    </p>
</div>

Donk is a lightweight cli for syncing development configs and libraries with an OSS-backed source.

Configure global settings in `~/.donk/settings.json`.

```json
{
  "version": 1,
  "oss": {
    "name": "aliyun-oss",
    "access_key": "your-access-key",
    "secret_key": "your-secret-key",
    "bucket": "your-bucket",
    "endpoint": "your-endpoint"
  },
  "cfg": [
    {
      "name": "nvim",
      "oss": "oss://your-bucket/donk/cfg/nvim",
      "link": "~/.config/nvim",
      "cmd": ["echo nvim config pulled"]
    },
    {
      "name": "tmux",
      "oss": "oss://your-bucket/donk/cfg/tmux",
      "link": "~/.config/tmux",
      "cmd": [
        "ln s ~/.tmux.conf ~/.config/tmux/tmux.conf",
        "echo tmux config pulled"
      ]
    }
  ]
}
```

After defining entries like `nvim` in global settings, use `donk cfg push` to upload local changes to OSS and `donk cfg pull` to sync the latest remote version.

```shell
donk cfg push nvim
donk cfg pull nvim
```
