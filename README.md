<p align="center">
  <picture>
    <source
      srcset="https://assets.husonym.com/husonym/docs/husonym-header-dark.svg"
      media="(prefers-color-scheme: dark)"
    />
    <img
      alt="Husonym"
      src="https://assets.husonym.com/husonym/docs/husonym-header.svg"
    />
  </picture>
</p>

<p align="center" style="font-size: 24px;font-weight: 500;">
Data Anonymization and Synthetic Data Orchestration
<p>

<div align='center'>
 | <a href="https://www.husonym.com">Website</a>
 | <a href="https://docs.husonym.com">Docs</a>
</div>

 <br>

<div align="center">
  <a href="https://github.com/fishtre-compagnie/husonym/blob/main/LICENSE.md">
    <img alt="License" src="https://img.shields.io/github/license/fishtre-compagnie/husonym" />
  </a>
  <a href="https://github.com/fishtre-compagnie/husonym/actions/workflows/go.yml">
    <img alt="Go Tests" src="https://github.com/fishtre-compagnie/husonym/actions/workflows/go.yml/badge.svg"/>
  </a>
</div>


## Introduction

[Husonym](https://www.husonym.com) is a way to anonymize PII, generate synthetic data and sync environments for better testing, debugging and developer experience.

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
- [GitHub Issues](https://github.com/fishtre-compagnie/husonym/issues) to report a bug or request a feature
- [CONTRIBUTING.md](./CONTRIBUTING.md) to get set up for local development
- <support@husonym.com> to reach us directly

## Licensing

We strongly believe in free and open source software and make this repo available under the [MIT expat license](./LICENSE.md).
