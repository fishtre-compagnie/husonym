---
title: Installing
description: Learn how to install the Husonym CLI onto your operating system of choice.
id: installing
hide_title: false
slug: /cli/installing
---

# Installing the CLI

Instructions on how to get the Husonym CLI installed on your local machine.

Husonym is delivered through a Command Line Interface (CLI) to make it easy for developers to use in their native workflows.
The Husonym CLI lets you view accounts, jobs and sync data locally. To get started with Husonym, follow the instructions below to download the CLI.

The CLI is distributed as a signed release archive. Download the one matching your platform, verify it, and put it on your `PATH`.

## MacOS/Linux Direct Download

Navigate to Husonym [releases](https://github.com/fishtre-compagnie/husonym/releases) page of the Husonym repository on GitHub. From there you can choose which binary to download based on your machine's architecture.

After you've downloaded and untarred the tarball, move it into your local bin to make it easy to run. If you're using Windows 10/11, see the Windows section below for more details.

**Note: the version listed below may not be the latest. Refer to the Releases page in the link above to retrieve the latest version of the binary.**

```console
tar xzf husonym_0.2.14_darwin_arm64.tar.gz husonym
mv husonym /usr/local/bin/husonym
```

### Verifying the download is genuine

Every release ships a `husonym_<version>_SHA256SUMS` listing the SHA-256 of each artifact,
and a `husonym_<version>_SHA256SUMS.sig` signing that file with our release key. Checking
both tells you the binary is the one we published and has not been altered in transit.

Import the key once. It is published in the repository at
[`cli/release-signing-key.asc`](https://github.com/fishtre-compagnie/husonym/blob/main/cli/release-signing-key.asc),
and its fingerprint is:

```
8585 8A2C F1D8 0B7D D030  D21A F31E ADE2 B79F 906B
```

```console
curl -fsSL https://raw.githubusercontent.com/fishtre-compagnie/husonym/main/cli/release-signing-key.asc | gpg --import
```

Then, from the directory holding the downloaded artifacts:

```console
gpg --verify husonym_0.1.1_SHA256SUMS.sig husonym_0.1.1_SHA256SUMS
sha256sum --check --ignore-missing husonym_0.1.1_SHA256SUMS
```

The first command must report a good signature from `Husonym Release Signing`; the second
must report `OK` for your archive. `gpg` will also warn that the key is not certified by a
trusted signature — that is expected, and why the fingerprint above is worth comparing.

### Verifying your installation

Once you've successfully installed the CLI, verify your installation by following these steps:

1. Open a new terminal window.
2. Type in `husonym help` into your terminal and press enter.
3. If installed successfully, you will see something similar to this help menu

```console
husonym

Terminal UI that interfaces with the Husonym system.

Usage:
  husonym [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  jobs        Parent command for jobs
  login       Login to Husonym
  sync        One off sync job to local resource
  version     Print the client version information
  whoami      Find out who you are

Flags:
      --api-key string   Husonym API Key. Takes precedence over $HUSONYM_API_KEY
      --config string    config file (default is $HOME/.husonym/husonym.yaml)
  -h, --help             help for husonym
  -v, --version          version for husonym

Use "husonym [command] --help" for more information about a command.
```

Now that you've successfully downloaded the Husonym CLI, you're ready to start building and deploying services. Check out the next section to get familiar with the Husonym CLI commands.

## Windows 10/11 Direct Download

Navigate to Husonym [releases](https://github.com/fishtre-compagnie/husonym/releases) page of the CLI repository in the Husonym Github. From there you can choose which binary to download based on your machine's architecture for Windows. Some examples are listed below.

After the download has completed, unzip the contents into a new folder. The most important file is husonym.exe. This can be left here, but a more appropriate place to move it would be to a folder such as: `C:\Husonym` or `C:\Apps\Husonym`

Afterwards, this location needs to be added to the system path. This can be done by going into Settings, and searching for "environment variables" in the search bar. Click "Edit Environment Variables for your Account".

The "Path" variable should be edited. User or System is dependent on your preferences.

### Verifying your installation

Once you've successfully installed the CLI, verify your installation by following these steps:

1. Open a new terminal window.
   - Note: the examples below are using Powershell 5.1.x on Windows 11
2. Type in `husonym help` into your terminal and press enter.
3. If installed successfully, you will see something similar to this help menu

```console
husonym
Terminal UI that interfaces with the Husonym system.

Usage:
  husonym [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  jobs        Parent command for jobs
  login       Login to Husonym
  sync        One off sync job to local resource
  version     Print the client version information
  whoami      Find out who you are

Flags:
      --api-key string   Husonym API Key. Takes precedence over $HUSONYM_API_KEY
      --config string    config file (default is $HOME/.husonym/husonym.yaml)
  -h, --help             help for husonym
  -v, --version          version for husonym

Use "husonym [command] --help" for more information about a command.
```
