<p align="center">
  <!-- <img alt="husonymbanner" src="https://assets.fishtre-compagnie.com/husonym/docs/husonym-header.svg" > -->
  <picture>
  <source
    srcset="https://assets.fishtre-compagnie.com/husonym/docs/husonym-header.svg"
    media="(prefers-color-scheme: light)"
  />
  <source
    srcset="https://assets.fishtre-compagnie.com/husonym/docs/husonym-header-dark.svg"
    media="(prefers-color-scheme: dark), (prefers-color-scheme: no-preference)"
  />
  <img src="https://github-readme-stats.vercel.app/api?username=anuraghazra&show_icons=true" />
</picture>
</p>

<p align="center" style="font-size: 24px;font-weight: 500;">
Data Anonymization and Synthetic Data Orchestration
<p>

<div align='center'>
 | <a href="https://www.husonym.com">Website</a>
 | <a href="https://docs.husonym.com">Docs</a>
 | <a href="https://www.husonym.com/blog">Blog</a>
 | <a href="https://docs.husonym.com/changelog">Changelog</a>
</div>

 <br>

<div align="center">
  <a href='https://makeapullrequest.com'>
    <img alt='PRs Welcome' src='https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=shields'/>
  </a>
  <img src="https://img.shields.io/github/license/lightdash/lightdash" />
  <!-- <a href="https://codecov.io/gh/fishtre-compagnie/husonym">
    <img alt="CodeCov" src="https://codecov.io/gh/fishtre-compagnie/husonym/graph/badge.svg?token=A35QDLRU04"/>
    </a> -->
  <a href="https://github.com/fishtre-compagnie/husonym/actions/workflows/go.yml/">
    <img alt="Go Tests" src="https://github.com/fishtre-compagnie/husonym/actions/workflows/go.yml/badge.svg"/>
  </a>
  <a href="https://x.com/husonymcloud">
    <img alt="Follow X" src="https://img.shields.io/twitter/follow/husonymcloud?label=Follow"/>
  </a>
  <a href="https://artifacthub.io/packages/search?repo=husonym">
    <img alt="ArtifactHub Husonym" src="https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/husonym" />
  </a>
  <a href="https://gurubase.io/g/husonym">
    <img alt="Gurubase" src="https://img.shields.io/badge/Gurubase-Ask%20Husonym%20Guru-006BFF" />
  </a>
</div>


## Introduction

[Husonym](https://www.husonym.com) is an way to anonymize PII, generate synthetic data and sync environments for better testing, debugging and developer experience.

Companies use Husonym to:

1. **Safely test code against production data** - Anonymize sensitive production data in order to safely use it locally for a better testing and developer experience
2. **Easily reproduce production bugs locally** - Anonymize and subset production data to get a safe, representative data set that you can use to locally reproduce production bugs quickly and efficiently
3. **High quality data for lower-level environments** - Catch bugs before they hit production when you hydrate your staging and QA environments with production-like data
4. **Solve GDPR, DPDP, FERPA, HIPAA and more** - Use anonymized and synthetic data to reduce your compliance scope and easily comply with laws like HIPAA, GDPR, and DPDP
5. **Seed development databases** - Easily seed development databases with synthetic data for unit testing, demos and more

## Features

- **Generate synthetic data** based on your schema
- **Anonymize existing production-data** for a better developer experience
- **Subset your production database** for local and CI testing using any SQL query
- **Complete async pipeline** that automatically handles job retries, failures and playback using an event-sourcing model
- **Referential integrity** for your data automatically
- **Declarative, GitOps based configs** as a step in your CI pipeline to hydrate your CI DB
- **Pre-built data transformers** for all major data types
- **Custom data transformers** using javascript or LLMs
- **Pre-built integrations** with Postgres, Mysql, S3

## Getting started

Husonym is a fully dockerized setup which makes it easy to get up and running.

A [compose.yml](./compose.yml) file at the root contains production image refs that allow you to get up and running with just a few commands without having to build anything on your system.

Husonym uses the newer `docker compose` command, so be sure to have that installed on your machine.

To start Husonym, clone the repo into a local directory, be sure to have docker installed and running, and then run:

```sh
make compose/up
```

To stop, run:

```sh
make compose/down
```

Husonym will now be available on [http://localhost:3000](http://localhost:3000).

The production compose pre-seeds with connections and jobs to get you started! Simply run the generate and sync job to watch Husonym in action!

## Kubernetes, Auth Mode and more

For more in-depth details on environment variables, Kubernetes deployments, and a production-ready guide, check out the [Deploy Husonym](https://docs.husonym.com/deploy/introduction) section of our Docs.

## Resources

Some resources to help you along the way:

- [Docs](https://docs.husonym.com) for comprehensive documentation and guides


## Licensing

We strongly believe in free and open source software and make this repo is available under the [MIT expat license](./LICENSE.md).
