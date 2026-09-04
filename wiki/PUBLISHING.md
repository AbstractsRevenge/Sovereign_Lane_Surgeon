<!--
Copyright 2026 Terrance Leverette (AbstractsRevenge)
Sovereign Lane Surgeon: https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Publishing these pages to the GitHub Wiki

These files are the **source** of the project wiki. They live in the repository so they are
reviewable, versioned and covered by the licence test; GitHub's wiki is a separate git repository
that carries no history worth keeping on its own.

## One-time setup

GitHub does not create a wiki's git repository until the wiki has its first page, and there is no
API for it. So once, by hand:

1. Open <https://github.com/AbstractsRevenge/Sovereign_Lane_Surgeon/settings> and make sure
   **Wikis** is enabled under Features.
2. Open the repository's **Wiki** tab and click **Create the first page**. Any content will do;
   the sync below overwrites it.

After that, `sovereign_lane_surgeon.wiki.git` exists and can be pushed to like any repository.

## Sync

```bash
cd /tmp && rm -rf sls-wiki
git clone git@github.com:AbstractsRevenge/Sovereign_Lane_Surgeon.wiki.git sls-wiki
cp /path/to/sovereign_lane_surgeon/wiki/*.md sls-wiki/
cd sls-wiki && rm -f PUBLISHING.md          # this file is not a wiki page
git add -A && git commit -m "Sync wiki from the repository" && git push
```

## Page naming

A file name becomes the page name, with hyphens shown as spaces: `Quick-Start.md` is the page
**Quick Start**, and `[[Quick Start]]` links to it. `_Sidebar.md` is special and renders as the
sidebar on every page. `Home.md` is the landing page.

## What belongs here, and what does not

These pages **explain**. They must not restate anything that changes per build: per-device status,
run identifiers and current results belong in `CURRENT_STATE.md`, which is the authoritative
record, and the wiki links to it. Duplicating volatile state across two places is how documentation
starts lying, which is the failure `docs_test.go` exists to prevent inside the repository.
