# Releasing & going public

This is the activation runbook for taking reactive-policy public. Everything in
the repo (README, docs site, Helm chart, Artifact Hub metadata) is prepared to
work the moment these switches are flipped.

## 1. Make the repository public

GitHub → repo **Settings → General → Danger Zone → Change visibility → Public**.

## 2. Protect the `main` branch

Branch protection is only available once the repo is public (free plan) or on
GitHub Pro/Team. Apply it immediately after step 1:

```bash
gh api repos/Vedooo/reactive-policy/branches/main/protection -X PUT --input - <<'JSON'
{
  "required_status_checks": { "strict": true, "contexts": ["Go", "YAML", "Markdown", "Shell", "Actions"] },
  "enforce_admins": false,
  "required_pull_request_reviews": { "required_approving_review_count": 0, "dismiss_stale_reviews": true, "require_code_owner_reviews": false },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
JSON
```

This requires a PR with passing CI for every change to `main`, blocks
force-pushes and deletions, and keeps history linear. Raise
`required_approving_review_count` to `1` once there are co-maintainers.

## 3. Make the GHCR packages public

The release workflow already pushes both packages to GHCR; make them public so
anonymous `helm install` and image pulls work:

- `ghcr.io/vedooo/reactive-policy` (operator image)
- `ghcr.io/vedooo/charts/reactive-policy` (Helm chart, OCI)

For each: GitHub → your profile → **Packages** → package → **Package settings →
Change visibility → Public**.

Verify:

```bash
helm show chart oci://ghcr.io/vedooo/charts/reactive-policy   # works unauthenticated
```

## 4. Enable the documentation site (GitHub Pages)

GitHub → repo **Settings → Pages → Build and deployment → Source = GitHub
Actions**. The `Docs` workflow (`.github/workflows/docs.yaml`) builds the
mkdocs-material site and deploys it on every push to `main` that touches `docs/`
or `mkdocs.yml`. Trigger it once via **Actions → Docs → Run workflow**.

Site: <https://vedooo.github.io/reactive-policy>

## 5. List on Artifact Hub

1. Sign in at <https://artifacthub.io> and **Add repository**:
   - Kind: **Helm charts**
   - URL: `oci://ghcr.io/vedooo/charts/reactive-policy`
2. Copy the generated **repository ID** into `artifacthub-repo.yml` (`repositoryID`).
3. Claim ownership by publishing that metadata. For the OCI chart, push it with
   [oras](https://oras.land):

   ```bash
   oras push ghcr.io/vedooo/charts/reactive-policy:artifacthub.io \
     --config /dev/null:application/vnd.cncf.artifacthub.config.v1+yaml \
     artifacthub-repo.yml:application/vnd.cncf.artifacthub.repository-metadata.layer.v1.yaml
   ```

4. The package page renders from the chart's `artifacthub.io/*` annotations
   (links, images, CRDs, operator capabilities). The README badge resolves once
   the repo is indexed.

## 6. (Optional) Cut the next release

Target resolution is in `CHANGELOG.md` under `[Unreleased]`. To release it:

1. Move the `[Unreleased]` entries under a new `## [0.2.0] - <date>` heading.
2. Bump `version` and `appVersion` in `charts/reactive-policy/Chart.yaml` and the
   `artifacthub.io/images` tag.
3. Tag and push: `git tag v0.2.0 && git push origin v0.2.0`. The `release`
   workflow builds the binaries, multi-arch image, and OCI chart automatically.

## 7. Backfilling old image tags

Releases cut before v0.2.1 published the container image only under the
`v`-prefixed tag (e.g. `v0.1.0`). The Helm chart's default `image.tag` matches
`appVersion` (no `v`), so an install of those older chart versions tries to
pull `ghcr.io/vedooo/reactive-policy:0.1.0`, which never existed. Artifact Hub
reports this as "image not found" during vulnerability scanning.

To create the missing v-stripped tag without rebuilding (it copies the same
multi-arch manifest, so the digest is preserved):

1. **Actions → Retag image → Run workflow**.
2. Set `version` to the bare semver (e.g. `0.1.0`). Leave `source-tag` blank
   to default to `v<version>`. Leave `also-latest` **off** unless you are
   retagging the current Latest release.
3. Verify anonymously:

   ```bash
   curl -fsS "https://ghcr.io/token?service=ghcr.io&scope=repository:vedooo/reactive-policy:pull" \
     | jq -r .token \
     | xargs -I{} curl -fsS -H "Authorization: Bearer {}" \
       "https://ghcr.io/v2/vedooo/reactive-policy/manifests/0.1.0" -o /dev/null -w "%{http_code}\n"
   ```

   `200` means the chart's default image reference now resolves and Artifact
   Hub will pick it up on the next scan.
