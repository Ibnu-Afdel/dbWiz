# AUR packaging

`dbwiz-bin/` packages the prebuilt static binaries from the GitHub release, with
shell completions for bash, zsh, and fish. Users install it with any AUR helper
(`yay -S dbwiz-bin`), or on Omarchy with `omarchy pkg aur add dbwiz-bin`.

## First publish

1. Create an account at <https://aur.archlinux.org> and add your SSH public key
   under *My Account*.
2. Clone the (empty) package repo and copy the files in:

   ```bash
   git clone ssh://aur@aur.archlinux.org/dbwiz-bin.git /tmp/dbwiz-bin
   cp packaging/aur/dbwiz-bin/{PKGBUILD,.SRCINFO,dbwiz-bin.install} /tmp/dbwiz-bin/
   cd /tmp/dbwiz-bin && git add -A && git commit -m "dbwiz-bin 1.1.0" && git push
   ```

## Each release

After the release workflow has published the new tag's binaries:

```bash
cd packaging/aur/dbwiz-bin
sed -i 's/^pkgver=.*/pkgver=1.2.0/; s/^pkgrel=.*/pkgrel=1/' PKGBUILD   # new version
updpkgsums                         # refresh the sha256sums from the release (pacman-contrib)
makepkg -f && makepkg --printsrcinfo > .SRCINFO   # build-check, then regenerate metadata
```

Commit the change here, then copy `PKGBUILD` and `.SRCINFO` into the AUR clone
and push it.
