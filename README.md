<picture>
  <source media="(prefers-color-scheme: dark)" srcset=".github/assets/banner-dark.png">
  <img src=".github/assets/banner.png" alt="GitHub-Runner" width="100%">
</picture>

<p align="center">
  <a href="https://github.com/junkerderprovinz/github-runner/actions/workflows/build.yml"><img src="https://img.shields.io/github/actions/workflow/status/junkerderprovinz/github-runner/build.yml?branch=main&label=Build&style=for-the-badge&logo=githubactions&logoColor=white" alt="Build" height="36"></a>&nbsp;
  <a href="https://github.com/myoung34/docker-github-actions-runner"><img src="https://img.shields.io/badge/Upstream-myoung34-db61a2?style=for-the-badge&logo=docker&logoColor=white" alt="Upstream myoung34/github-runner" height="36"></a>&nbsp;
  <a href="https://ca.unraid.net/apps?q=github-runner"><img src="https://img.shields.io/badge/Unraid-Template-f15a2c?style=for-the-badge&logo=unraid&logoColor=white" alt="Unraid" height="36"></a>&nbsp;
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-GPL--3.0-yellow?style=for-the-badge&logo=gnu&logoColor=white" alt="License" height="36"></a>
</p>

<p align="center">
Self-hosted <b>GitHub Actions runners</b> for several repositories in <b>one container</b>.
A personal GitHub account can attach a runner to a single repository only, so the
usual images need one container per repository. This one starts a runner for every
repository you list, and removes them from GitHub again when it stops.
</p>

<br>

<p><a id="table-of-contents" href="#table-of-contents"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/table-of-contents-dark.svg"><img src=".github/assets/sections/table-of-contents.svg" alt="Table of Contents" width="100%"></picture></a></p>

1. [Download](#1-download)
2. [Support](#2-support)
3. [What it looks like](#3-what-it-looks-like)
4. [What it does](#4-what-it-does)
5. [Getting started](#5-getting-started)
6. [How AI is used here](#6-how-ai-is-used-here)

<br>

<p><a id="1-download" href="#1-download"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/1-download-dark.svg"><img src=".github/assets/sections/1-download.svg" alt="1. Download" width="100%"></picture></a></p>

<!-- download-buttons: written by scripts/gen_download_buttons.py -->
<p align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/group-server-dark.svg"><img src=".github/assets/sections/group-server.svg" alt="Server" width="100%"></picture>
</p>
<p align="center">
  <a href="https://ca.unraid.net/apps?q=github-runner"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(0,0,841.9,245.3))" alt="Install from Unraid&#x27;s Community Applications" width="160" height="46.618"></a>
  &nbsp;
  <a href="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/compose.yaml"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(866,0,841.9,245.3))" alt="Download the docker-compose file" width="160" height="46.618"></a>
  &nbsp;
  <a href="https://github.com/junkerderprovinz/github-runner/releases/latest"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(1732,0,841.9,245.3))" alt="Download the source archive" width="160" height="46.618"></a>
</p>
<!-- /download-buttons -->

<br>

<p><a id="2-support" href="#2-support"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/2-support-dark.svg"><img src=".github/assets/sections/2-support.svg" alt="2. Support" width="100%"></picture></a></p>

<p align="center">
A one-knight job: I build it, keep it running, work through the issues and add what people ask for, until nothing is missing. It is free, with no accounts, no telemetry, no ads and no paid tier. No asterisk anywhere. Nothing readable ever leaves your own walls. Forged on evenings and weekends, with heart and stubbornness.
</p>

<p align="center">
If it has earned a place on your server or computer, toss a coin to your knight: it helps cover the costs and keeps the project alive. It also makes this knight's heart beat a little faster. Three ways below, whichever suits you.
</p>

<!-- give-buttons: written by scripts/gen_download_buttons.py -->
<p align="center">
  <a href="https://buymeacoffee.com/junkerderprovinz"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(2598,0,841.9,245.3))" alt="Buy me a coffee" width="160" height="46.618"></a>
  &nbsp;
  <a href="https://www.paypal.com/donate/?hosted_button_id=76FVV52TKXTUS"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(3464,0,841.9,245.3))" alt="PayPal" width="160" height="46.618"></a>
  &nbsp;
  <a href="https://junkerderprovinz.github.io/junkerderprovinz/"><img src="https://raw.githubusercontent.com/junkerderprovinz/github-runner/main/.github/assets/download-buttons/buttons.svg?v=105fae391c19#svgView(viewBox(4330,0,841.9,245.3))" alt="Donate with crypto" width="160" height="46.618"></a>
</p>
<!-- /give-buttons -->

<br>

<p><a id="3-what-it-looks-like" href="#3-what-it-looks-like"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/3-what-it-looks-like-dark.svg"><img src=".github/assets/sections/3-what-it-looks-like.svg" alt="3. What it looks like" width="100%"></picture></a></p>

The repository names in this picture are made up.

<p align="center">
  <img src=".github/assets/screenshots/github-runner-1.png" alt="The container log after a start: the banner, then three repositories each registered and listening for jobs" width="100%">
  <br><em>The log after a start with three repositories: each one gets its own runner, all listening for jobs</em>
</p>

<br>

<p><a id="4-what-it-does" href="#4-what-it-does"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/4-what-it-does-dark.svg"><img src=".github/assets/sections/4-what-it-does.svg" alt="4. What it does" width="100%"></picture></a></p>

- **One container, many repositories.** List them in `RUNNER_REPOS` and each one gets its own runner. On a personal account that is the only way to share a machine between repositories, short of moving them into an organization.
- **Clean up on stop.** Stopping the container removes every runner from GitHub, so the Actions settings of your repositories never fill up with offline leftovers. Starting it registers them again.
- **One bad entry does not stop the rest.** A repository the token cannot reach is reported in the log and tried again every five minutes, while the others keep running.
- **Clear messages.** A missing token, an empty list or a name without an owner stops the container with one line that says what is wrong.
- **Docker in your jobs.** Mount the Docker socket and the runners can build images and start containers, as in GitHub's own runners.
- **Root or not.** The runners run as root by default, like the upstream image. With `RUN_AS_ROOT=false` they run as the unprivileged `runner` user.
- **Healthy means listening.** The health check turns red as soon as one of the listed repositories has no runner waiting for jobs.
- **Built on [myoung34/github-runner](https://github.com/myoung34/docker-github-actions-runner).** The runner, its tools and its updates come from there. This image adds the script that runs several of them.

<br>

<p><a id="5-getting-started" href="#5-getting-started"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/5-getting-started-dark.svg"><img src=".github/assets/sections/5-getting-started.svg" alt="5. Getting started" width="100%"></picture></a></p>

You need a personal access token that may manage runners in every repository you list:

- **Fine-grained token:** repository access to those repositories, permission **Administration: Read and write**.
- **Classic token:** the `repo` scope.

On Unraid, install GitHub-Runner from [Community Applications](https://ca.unraid.net/apps?q=github-runner), paste the token and list the repositories. Anywhere else, one container is enough:

```sh
docker run -d --name github-runner \
  -e ACCESS_TOKEN=github_pat_... \
  -e RUNNER_REPOS="you/website you/backend you/dotfiles" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --stop-timeout 60 \
  junkerderprovinz/github-runner:latest
```

Then point a workflow at it with `runs-on: self-hosted`. Each runner shows up under **Settings → Actions → Runners** of its repository, named `GitHub-Runner`.

| Variable | Default | What it does |
| --- | --- | --- |
| `ACCESS_TOKEN` | | Personal access token, see above. Required. |
| `RUNNER_REPOS` | | Repositories as `owner/name`, separated by spaces or commas. Required. |
| `REPO_OWNER` | | Owner for entries written without one, so `RUNNER_REPOS=website backend` works. |
| `RUNNER_NAME` | `GitHub-Runner` | Name of the runner in every repository. |
| `LABELS` | `self-hosted` | Extra labels, comma-separated. |
| `NO_DEFAULT_LABELS` | `false` | `true` drops GitHub's default labels `self-hosted`, `Linux` and `X64`. |
| `RUN_AS_ROOT` | `true` | `false` runs the runners as the `runner` user. |
| `DEREGISTER_ON_STOP` | `true` | `false` keeps the runners registered while the container is stopped. |
| `RETRY_SECONDS` | `300` | How long to wait before trying a repository again that failed. |

Give the container a minute to stop (`--stop-timeout 60`, already set in the Unraid template). Removing the runners takes a moment per repository, and Docker's default of ten seconds can cut that short with many of them.

A self-hosted runner runs whatever the workflow in that repository tells it to. Use it for your own repositories, not for public ones where strangers can open pull requests.

### Other Docker hosts

[compose.yaml](compose.yaml) runs GitHub-Runner on any Linux server with Docker. Put it in a folder of its own, fill in the token and the repositories, and start it with `docker compose up -d`.

- **Synology:** in Container Manager, create a project from the file (DSM 7.2 or newer).
- **Proxmox:** run it in a VM or an LXC container that has Docker.

<br>

<p><a id="6-how-ai-is-used-here" href="#6-how-ai-is-used-here"><picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/sections/6-how-ai-is-used-here-dark.svg"><img src=".github/assets/sections/6-how-ai-is-used-here.svg" alt="6. How AI is used here" width="100%"></picture></a></p>

One knight builds this, and AI is one of the tools I work with, the same way I work with an editor or a compiler. It helps me write code and documentation and it checks my work, and that saves me a good many evenings. It does not make the decisions, though. I read and understand everything before it ships, and if something here breaks, that is on me and not on the tool.

You do not have to take my word for it. The code is open and every release note is written by hand. The issue tracker shows how problems actually get handled, including the ones I got wrong the first time. If you find something that is not right, open an issue and I will look at it.

<br>

Questions, bugs, ideas or feature requests? Please [open a GitHub issue](https://github.com/junkerderprovinz/github-runner/issues).

<sub>The runner image is made by myoung34 and the GitHub Actions runner by GitHub. Both ship here unmodified. GitHub and the Octocat are trademarks of GitHub, Inc. This project is not affiliated with GitHub.</sub>
