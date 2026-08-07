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

## MacOS

Homebrew is the simplest way to install the Husonym CLI on the Mac. This can also be used on Linux, as well as on Windows 10 with Windows Subsystem for Linux.

### Homebrew

The easiest way to install the CLI is by using Homebrew. If you don't have Homebrew installed, follow these [instructions](https://docs.brew.sh/Installation). Next, open a new terminal window and use the following command:

```console
brew install husonym
```

You may also install directly from our brew repository:

```console
brew install fishtre-compagnie/tap/husonym
```

From then on, you can let Homebrew keep Husonym up to date by running the following command.

```console
brew upgrade
```

## MacOS/Linux Direct Download

Navigate to Husonym [releases](https://github.com/fishtre-compagnie/husonym/releases) page of the Husonym repository on GitHub. From there you can choose which binary to download based on your machine's architecture.

After you've downloaded and untarred the tarball, move it into your local bin to make it easy to run. If you're using Windows 10/11, see the Windows section below for more details.

**Note: the version listed below may not be the latest. Refer to the Releases page in the link above to retrieve the latest version of the binary.**

```console
tar xzf husonym_0.2.14_darwin_arm64.tar.gz husonym
mv husonym /usr/local/bin/husonym
```

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

## Docker

A Docker image is published that matches each official release of Husonym CLI. Each versioned image includes the Husonym CLI release with the same version number.

These images wrap the Husonym executable, allowing you to run Husonym subcommands by passing in their names and arguments as part of `docker run`.

The list of images can be found on [Github](https://github.com/fishtre-compagnie/husonym/pkgs/container/husonym%2Fcli).

### Configuration

The container will need further configuration so that Husonym can access configuration files, and possibly source code if the plan is to issue source-code deployments with Husonym CLI.

See the example below for how to login to the CLI, and then view a list of environments in a Husonym account.

```console
docker run -it --rm -p 4242:4242 --mount source=husonymcfg,target=/root/.config/.husonym ghcr.io/fishtre-compagnie/husonym/cli:latest login
```

```console
docker run -it --rm --mount source=husonymcfg,target=/root/.config/.husonym ghcr.io/fishtre-compagnie/husonym/cli:latest accounts ls
```

The command above will print out a list of environments that are in the account associated with the logged in credentials. Note that the port mapping isn't required here, as that is only necessary during the login flow.

The docker volume is necessary in order to persist the Husonym CLI configuration data between runs. This today is namely used to persist auth data used during the login process so that it can be used with the other CLI commands.
