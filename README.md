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
      "link": [
        "~/.config/nvim"
      ],
      "cmd": ["echo nvim config pulled"]
    },
    {
      "name": "tmux",
      "link": [
        "~/.config/tmux",
        "~/.config/tmux/tmux.conf -> ~/.tmux.conf"
      ],
      "cmd": [
        "ln s ~/.tmux.conf ~/.config/tmux/tmux.conf",
        "echo tmux config pulled"
      ]
    }
  ]
}
```

`cfg[].oss` and `lib[].oss` are optional.
- Default cfg path: `oss://<oss.bucket>/donk/cfg/<cfg.name>`
- Default lib path: `oss://<oss.bucket>/donk/lib/<lib.name>`
- You can still set `oss` explicitly per entry to override the default.
- `cfg[].link` and `lib[].link` both support:
  - `"<link>"` which means `<link> -> ~/.donk/{cfg|lib}/<name>`
  - `"<link> -> <src>"` which means `<link> -> <src>`

After defining entries like `nvim` in global settings, use `donk cfg push` to upload local changes to OSS and `donk cfg pull` to sync the latest remote version.
For first-time migration (for example from `~/.config/nvim`), use `donk cfg init`.

```shell
donk cfg init nvim
donk cfg push nvim
donk cfg pull nvim
```
