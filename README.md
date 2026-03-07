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

### Installing

This assumes you already have a working Go environment, if not please see
[this page](https://golang.org/doc/install) first.

`go get` *will always pull the latest tagged release from the master branch.*

```sh
go get github.com/bwmarrin/discordgo
```

### Usage

Import the package into your project.

```go
import "github.com/bwmarrin/discordgo"
```

Construct a new Discord client which can be used to access the variety of 
Discord API functions and to set callback functions for Discord events.

```go
discord, err := discordgo.New("Bot " + "authentication token")
```

See Documentation and Examples below for more detailed information.


## DAVE (Voice E2EE) Support

This fork includes support for the [DAVE protocol](https://daveprotocol.com/) (Discord Audio & Video End-to-End Encryption). DAVE adds frame-level E2EE on top of the existing transport encryption.

### Prerequisites

DAVE requires [libdave](https://github.com/discord/libdave) (Discord's official C++ library) to be built and available at link time. The following tools are needed:

- **Go 1.21+**
- **C/C++ compiler** (Clang or GCC)
- **CMake 3.16+**
- **Git** (for cloning and submodule init)
- **pkg-config** (`brew install pkg-config` on macOS)

### Building libdave

Clone and build libdave **next to** your discordgo directory (the CGo flags expect `../libdave` relative to the discordgo root):

```sh
# From the parent directory of discordgo
git clone https://github.com/discord/libdave.git
cd libdave
git submodule update --init --recursive

# Bootstrap vcpkg (bundled as a submodule)
cd cpp
./vcpkg/bootstrap-vcpkg.sh

# Build (OpenSSL 3.x variant)
make all SSL=openssl_3 BUILD_TYPE=Release

# Install headers and static library
make install SSL=openssl_3 BUILD_TYPE=Release
```

After this, you should have:
```
libdave/cpp/build/install/lib/libdave.a
libdave/cpp/build/install/include/dave/dave.h
libdave/cpp/build/vcpkg_installed/arm64-osx/lib/  (mlspp, ssl, crypto, etc.)
```

> **Note:** The vcpkg triplet directory name varies by platform (e.g., `arm64-osx`, `x64-linux`). If you are not on Apple Silicon macOS, update the `#cgo LDFLAGS` path in `dave/libdave.go` accordingly.

### Usage

```go
// Join a voice channel with DAVE E2EE enabled
vc, err := session.ChannelVoiceJoinE2EE(guildID, channelID, false, false)
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
