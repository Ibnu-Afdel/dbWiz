# AUR packaging

`dbwiz-bin/` packages the prebuilt static binaries from the GitHub release, with
shell completions for bash, zsh, and fish. Users install it with any AUR helper
(`yay -S dbwiz-bin`), or on Omarchy with `omarchy pkg aur add dbwiz-bin`.

Published at <https://aur.archlinux.org/packages/dbwiz-bin>; the package's git
repo is `ssh://aur@aur.archlinux.org/dbwiz-bin.git`. This directory is the source
of truth — the AUR repo holds copies of `PKGBUILD`, `.SRCINFO`, and
`dbwiz-bin.install`.

## Each release

After the release workflow has published the new tag's binaries:

```bash
cd packaging/aur/dbwiz-bin
sed -i 's/^pkgver=.*/pkgver=1.2.0/; s/^pkgrel=.*/pkgrel=1/' PKGBUILD   # new version
updpkgsums                         # refresh the sha256sums from the release (pacman-contrib)
makepkg -f && makepkg --printsrcinfo > .SRCINFO   # build-check, then regenerate metadata
```

Commit the change here, then publish it:

```bash
git clone ssh://aur@aur.archlinux.org/dbwiz-bin.git /tmp/dbwiz-bin
cp packaging/aur/dbwiz-bin/{PKGBUILD,.SRCINFO,dbwiz-bin.install} /tmp/dbwiz-bin/
cd /tmp/dbwiz-bin && git commit -am "Update to 1.2.0" && git push
```
