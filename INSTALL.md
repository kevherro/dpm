# Install dpm

dpm v1 supports macOS 14 or newer on Apple silicon and requires Git from the
Xcode Command Line Tools (`xcode-select --install`).

After verifying the release archive against `SHA256SUMS`, copy `dpm` to a
user-owned executable directory:

```sh
mkdir -p "$HOME/.local/bin"
cp dpm "$HOME/.local/bin/dpm"
export PATH="$HOME/.local/bin:$HOME/.dpm/bin:$PATH"
dpm update
```

dpm never requires `sudo` and never edits shell configuration.
