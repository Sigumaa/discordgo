# DiscordGo

[![Go Reference](https://pkg.go.dev/badge/github.com/bwmarrin/discordgo.svg)](https://pkg.go.dev/github.com/bwmarrin/discordgo) [![Go Report Card](https://goreportcard.com/badge/github.com/bwmarrin/discordgo)](https://goreportcard.com/report/github.com/bwmarrin/discordgo) [![CI](https://github.com/bwmarrin/discordgo/actions/workflows/ci.yml/badge.svg)](https://github.com/bwmarrin/discordgo/actions/workflows/ci.yml) [![Discord Gophers](https://img.shields.io/badge/Discord%20Gophers-%23discordgo-blue.svg)](https://discord.gg/golang) [![Discord API](https://img.shields.io/badge/Discord%20API-%23go_discordgo-blue.svg)](https://discord.com/invite/discord-api)

<img align="right" alt="DiscordGo logo" src="docs/img/discordgo.svg" width="400">

DiscordGo is a [Go](https://golang.org/) package that provides low level 
bindings to the [Discord](https://discord.com/) chat client API. DiscordGo 
has nearly complete support for all of the Discord API endpoints, websocket
interface, and voice interface.

If you would like to help the DiscordGo package please use 
[this link](https://discord.com/oauth2/authorize?client_id=173113690092994561&scope=bot)
to add the official DiscordGo test bot **dgo** to your server. This provides 
indispensable help to this project.

* See [dgVoice](https://github.com/bwmarrin/dgvoice) package for an example of
additional voice helper functions and features for DiscordGo.

* See [dca](https://github.com/bwmarrin/dca) for an **experimental** stand alone
tool that wraps `ffmpeg` to create opus encoded audio appropriate for use with
Discord (and DiscordGo).

**For help with this package or general Go discussion, please join the [Discord 
Gophers](https://discord.gg/golang) chat server.**

## Getting Started

This library uses CGo to link against [libdave](https://github.com/discord/libdave) for [DAVE protocol](https://daveprotocol.com/) (Discord Audio & Video E2EE) support. A C++ toolchain is required.

> **Note:** `go get` alone is not sufficient. You must clone this repository and build libdave first.

### 1. Install prerequisites

**macOS:**
```sh
brew install go cmake pkg-config
```

**Debian / Ubuntu:**
```sh
sudo apt-get install golang build-essential cmake pkg-config git curl zip unzip tar
```

**Fedora / RHEL:**
```sh
sudo dnf install golang gcc-c++ cmake pkgconf git
```

### 2. Clone and build

```sh
git clone https://github.com/sh1ma/discordgo.git
cd discordgo

# Build libdave and install dependencies (clones discord/libdave automatically)
./scripts/setup-dave.sh
```

The script will:
1. Clone [discord/libdave](https://github.com/discord/libdave) into `.libdave/`
2. Build it with vcpkg (OpenSSL 3.x by default)
3. Copy headers and static libraries into `dave/deps/`

Verify the build:
```sh
go build ./...
go test ./...
```

<details>
<summary>Script options</summary>

```sh
# Use a different SSL variant (openssl_3 | openssl_1.1 | boringssl)
./scripts/setup-dave.sh --ssl boringssl

# Use an already cloned libdave
./scripts/setup-dave.sh --libdave-dir /path/to/libdave
```
</details>

<details>
<summary>Manual setup (without the script)</summary>

```sh
# Clone and build libdave
git clone https://github.com/discord/libdave.git
cd libdave
git submodule update --init --recursive
cd cpp
./vcpkg/bootstrap-vcpkg.sh
make all SSL=openssl_3 BUILD_TYPE=Release
make install SSL=openssl_3 BUILD_TYPE=Release

# Copy artifacts into discordgo/dave/deps/
# Replace <triplet> with your platform (arm64-osx, x64-linux, etc.)
mkdir -p /path/to/discordgo/dave/deps/{include,lib}
cp -r cpp/build/install/include/dave /path/to/discordgo/dave/deps/include/
cp cpp/build/install/lib/libdave.a /path/to/discordgo/dave/deps/lib/
cp cpp/build/vcpkg_installed/<triplet>/lib/lib{mlspp,mls_vectors,mls_ds,bytes,tls_syntax,hpke,ssl,crypto}.a \
   /path/to/discordgo/dave/deps/lib/
```
</details>

### 3. Use in your project

In your bot's `go.mod`, add a `replace` directive pointing to your local clone:

```
require github.com/bwmarrin/discordgo v0.28.1

replace github.com/bwmarrin/discordgo => /path/to/discordgo
```

Then in your code:

```go
import "github.com/bwmarrin/discordgo"

// Create session
discord, err := discordgo.New("Bot " + token)

// Join voice with DAVE E2EE
vc, err := discord.ChannelVoiceJoinE2EE(guildID, channelID, false, false)
```

The DAVE handshake (MLS key exchange via voice opcodes 22/23/24) is handled automatically. When DAVE is not negotiated by the server, the connection falls back to standard transport encryption seamlessly.

## Documentation

**NOTICE**: This library and the Discord API are unfinished.
Because of that there may be major changes to library in the future.

The DiscordGo code is fairly well documented at this point and is currently
the only documentation available. Go reference (below) presents that information in a nice format.

- [![Go Reference](https://pkg.go.dev/badge/github.com/bwmarrin/discordgo.svg)](https://pkg.go.dev/github.com/bwmarrin/discordgo) 
- Hand crafted documentation coming eventually.


## Examples

Below is a list of examples and other projects using DiscordGo.  Please submit 
an issue if you would like your project added or removed from this list. 

- [DiscordGo Examples](https://github.com/bwmarrin/discordgo/tree/master/examples) - A collection of example programs written with DiscordGo
- [Awesome DiscordGo](https://github.com/bwmarrin/discordgo/wiki/Awesome-DiscordGo) - A curated list of high quality projects using DiscordGo

## Troubleshooting
For help with common problems please reference the 
[Troubleshooting](https://github.com/bwmarrin/discordgo/wiki/Troubleshooting) 
section of the project wiki.


## Contributing
Contributions are very welcomed, however please follow the below guidelines.

- First open an issue describing the bug or enhancement so it can be
discussed.  
- Try to match current naming conventions as closely as possible.  
- This package is intended to be a low level direct mapping of the Discord API, 
so please avoid adding enhancements outside of that scope without first 
discussing it.
- Create a Pull Request with your changes against the master branch.


## List of Discord APIs

See [this chart](https://abal.moe/Discord/Libraries.html) for a feature 
comparison and list of other Discord API libraries.

## Special Thanks

[Chris Rhodes](https://github.com/iopred) - For the DiscordGo logo and tons of PRs.
