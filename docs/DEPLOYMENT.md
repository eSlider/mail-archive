# Deploy Mail Archive — Free Tier Guide

This guide covers deploying the Mail Archive service on free hosting platforms, configuring GitHub Actions secrets for CI/CD, and creating the required accounts.

## Table of Contents

- [GitHub Actions Secrets Setup](#github-actions-secrets-setup)
- [Fly.io](#flyio)
- [OpenShift Developer Sandbox](#openshift-developer-sandbox)
- [AWS Free Tier](#aws-free-tier)
- [Other Platforms](#other-platforms)

---

## GitHub Actions Secrets Setup

Secrets are used by CI/CD workflows to authenticate with external services. **Never** commit secrets to the repository.

### Where to Configure Secrets

1. Go to your GitHub repository
2. **Settings** → **Secrets and variables** → **Actions**
3. Click **New repository secret**

### Required Secrets by Platform

| Secret Name       | Platform | Purpose |
|-------------------|----------|---------|
| `FLY_API_TOKEN`   | Fly.io   | Deploy apps via Fly CLI |
| `GHCR_TOKEN`      | GitHub   | Push images to GHCR (uses `GITHUB_TOKEN` by default) |
| `AWS_ACCESS_KEY_ID` | AWS   | Deploy to ECR/ECS/EC2 |
| `AWS_SECRET_ACCESS_KEY` | AWS | AWS authentication |
| `OPENSHIFT_TOKEN` | OpenShift | Deploy to OpenShift cluster |

### Environment Variables vs Secrets

- **Secrets**: Passwords, API keys, tokens — set in GitHub repo settings, never in workflow YAML
- **Environment variables**: Non-sensitive config (e.g. `BASE_URL`) — can be set in workflow or at runtime

### Usage in Workflows

Workflows reference secrets like this:

```yaml
env:
  FLY_API_TOKEN: ${{ secrets.FLY_API_TOKEN }}
```

If a secret is not configured, the step will fail. Use `continue-on-error: true` for optional deploy steps (e.g. when only local Docker is used).

### Links

- [GitHub: Encrypted secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets)
- [GitHub: Using secrets in workflows](https://docs.github.com/en/actions/security-guides/using-secrets-in-github-actions)

---

## Fly.io

**Free tier**: 3 shared-cpu VMs, 3GB persistent storage, 160GB outbound transfer/month.

### Create Account

1. Go to [fly.io/app/sign-up](https://fly.io/app/sign-up)
2. Sign up with email or GitHub
3. No credit card required for free tier

### Get API Token

1. Log in: `fly auth login`
2. Create token: [fly.io/user/tokens](https://fly.io/user/tokens) or run:
   ```bash
   fly tokens create deploy -x 9999999h
   ```
3. Add to GitHub: **Settings** → **Secrets** → `FLY_API_TOKEN`

### Deploy via CI/CD

1. Configure `FLY_API_TOKEN` secret (see above)
2. Create Fly app and volume (first time only):
   ```bash
   fly launch --no-deploy   # Creates app from fly.toml
   fly volumes create mail_archive_data --region ord --size 1
   ```
3. Trigger deployment:
   - **Manual**: Actions → Deploy → Run workflow
   - **On release**: Push a `v*` tag (e.g. `v1.0.1`)

### Links

- [Fly.io Docs](https://fly.io/docs/)
- [Fly.io Pricing](https://fly.io/docs/about/pricing/)
- [Fly.io Tokens](https://fly.io/docs/reference/tokens/)

---

## OpenShift Developer Sandbox

**Free tier**: Full OpenShift cluster, no credit card required.

### Create Account

1. Go to [developers.redhat.com/developer-sandbox](https://developers.redhat.com/developer-sandbox)
2. Sign in with Red Hat account (create one if needed)
3. Launch your sandbox — instant cluster provisioning

### Get Token

1. In the OpenShift web console, click your username (top right)
2. **Copy login command** → use the token from the `oc login` command
3. Add to GitHub secrets as `OPENSHIFT_TOKEN`

### Deploy (Manual)

```bash
oc login --token=... --server=...
oc new-app . --name=mail-archive
oc expose svc/mail-archive
```

For automated CI/CD, use the OpenShift GitHub Action or `oc` with the token.

### Links

- [Red Hat Developer Sandbox](https://developers.redhat.com/developer-sandbox)
- [OpenShift CLI (oc)](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html)

---

## AWS Free Tier

**Free tier**: 12 months — 750h/month EC2 t2.micro, 30GB EBS, 750h RDS (if needed).

### Create Account

1. Go to [aws.amazon.com/free](https://aws.amazon.com/free/)
2. Create AWS account (credit card required for verification; free tier stays free within limits)
3. Enable MFA for root account (recommended)

### Create IAM User for CI/CD

1. **IAM** → **Users** → **Create user** (e.g. `github-actions`)
2. Attach policy: `AmazonEC2ContainerRegistryFullAccess` and `AmazonECS_FullAccess` (or minimal custom policy)
3. **Security credentials** → **Create access key** → Application running outside AWS
4. Copy Access Key ID and Secret — add to GitHub as `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`

### Deploy Options

- **EC2**: Build on GitHub, SCP binary or use Docker + ECR
- **ECS Fargate**: Push image to ECR, create ECS service
- **Lightsail**: Simpler alternative with free tier

### Links

- [AWS Free Tier](https://aws.amazon.com/free/)
- [IAM Best Practices](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html)
- [GitHub Action: aws-actions/configure-aws-credentials](https://github.com/aws-actions/configure-aws-credentials)

---

## Other Platforms

| Platform      | Free Tier                 | Create Account                    |
|---------------|---------------------------|-----------------------------------|
| **Railway**   | ~$5 credit/month          | [railway.app](https://railway.app) |
| **Render**    | Free web services         | [render.com](https://render.com)  |
| **Google Cloud** | Always-free e2-micro   | [cloud.google.com/free](https://cloud.google.com/free) |
| **Oracle Cloud** | Always-free VMs       | [oracle.com/cloud/free](https://www.oracle.com/cloud/free/) |

---

## Quick Reference: GitHub Secrets Checklist

Before running the Deploy workflow:

- [ ] `FLY_API_TOKEN` — from [fly.io/user/tokens](https://fly.io/user/tokens)
- [ ] (Optional) `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` — for AWS deploy
- [ ] (Optional) `OPENSHIFT_TOKEN` — for OpenShift deploy

OAuth (runtime, not CI/CD):

- [ ] `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` — for GitHub OAuth login
- [ ] `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` — for Google OAuth
- [ ] `FACEBOOK_CLIENT_ID` / `FACEBOOK_CLIENT_SECRET` — for Facebook OAuth

Set OAuth vars in your hosting platform’s environment (e.g. Fly.io secrets, AWS Parameter Store) — do **not** put them in GitHub Actions if they’re only needed at runtime.
