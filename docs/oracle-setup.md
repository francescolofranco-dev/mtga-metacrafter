# Hosting MTGA MetaCrafter on Oracle Cloud Always Free

Oracle Cloud's *Always Free* tier gives you compute and storage that never
expire — no trial clock, no policy-rug like Fly. The catch is that signing
up is more involved than Fly was, and account verification can take
anywhere from a few minutes to a few days.

This document walks you from zero to a running VM ready to receive a
deploy from this repo.

## 1. Sign up

1. Go to https://www.oracle.com/cloud/free/ and click **Start for free**.
2. Use a personal email (not Gmail aliases — Oracle sometimes rejects
   `+`-aliases in account creation).
3. **Home Region**: pick the region closest to most of your users.
   Common picks for European traffic: `eu-frankfurt-1` or
   `eu-amsterdam-1`. Once chosen, this can't be changed. The Always
   Free machine has to live in your home region.
4. Verify by SMS, then add a credit card. **You will not be charged
   unless you explicitly upgrade.** Always Free resources are billed
   at zero.
5. After submitting, your account may sit in a "pending verification"
   state. Worst-case this can take 1–3 days. You'll get an email when
   it's approved.

## 2. Create the VM (Always Free)

Once your account is active:

1. Open the Cloud Console → top-left hamburger → **Compute → Instances**.
2. Click **Create instance**.
3. Settings:
   - **Name**: `mtga-metacrafter`
   - **Image**: Click **Change image** → **Ubuntu** → **Canonical
     Ubuntu 24.04** (or whatever's latest LTS).
   - **Shape**: Click **Change shape** → **Ampere** → pick
     `VM.Standard.A1.Flex`. Drag OCPU to 1 and memory to 6 GB
     (well inside the Always Free 4-OCPU / 24-GB ARM allotment, and
     far more headroom than this app needs).
   - **Networking**: leave the default VCN. Make sure **Assign a
     public IPv4 address** is checked.
   - **Add SSH keys**: select **Generate a key pair for me** and
     download both the public *and* private key. Save them as
     `~/.ssh/mtga-metacrafter` and `~/.ssh/mtga-metacrafter.pub`.
     Then: `chmod 600 ~/.ssh/mtga-metacrafter`.
   - **Boot volume**: leave at default 50 GB (within the 200 GB
     Always Free quota).
4. Click **Create**. Provisioning takes 1–2 minutes.
5. When it's `RUNNING`, copy the **Public IP Address** from the
   instance details page.

> **If Oracle says "Out of capacity" on the ARM shape**: this is
> common in popular regions. Either retry later, or switch to a
> different region during sign-up, or fall back to the AMD shape
> `VM.Standard.E2.1.Micro` (1 OCPU, 1 GB RAM — still plenty for this app).

## 3. Open the firewall

By default Oracle blocks all inbound ports except SSH (22). We need
80 (HTTP, for Let's Encrypt) and 443 (HTTPS).

1. From the instance page click on the **VCN** link under "Primary VNIC".
2. Click the **Subnet** the VM is in, then the **Default Security List**.
3. **Add Ingress Rules**:
   - Source CIDR `0.0.0.0/0`, IP Protocol `TCP`, Destination Port
     `80`.
   - Source CIDR `0.0.0.0/0`, IP Protocol `TCP`, Destination Port
     `443`.

The OS firewall (`iptables`) also blocks these by default on
Oracle Ubuntu images. The `install.sh` script in `deploy/` opens
them for you.

## 4. First SSH

From your laptop:

```
ssh -i ~/.ssh/mtga-metacrafter ubuntu@<PUBLIC_IP>
```

(The default user on Ubuntu images is `ubuntu`.)

You should land in a fresh shell. If you get rejected, double-check
the SSH key permissions (`chmod 600 ~/.ssh/mtga-metacrafter`) and
confirm you're using the private key file, not the `.pub`.

## 5. Run the installer

From your laptop, copy the install playbook to the VM and run it:

```
scp -i ~/.ssh/mtga-metacrafter deploy/install.sh ubuntu@<PUBLIC_IP>:/tmp/
ssh -i ~/.ssh/mtga-metacrafter ubuntu@<PUBLIC_IP> "sudo bash /tmp/install.sh"
```

What this does on the VM:
- Opens 80/443 in `iptables`.
- Creates a system user `metacrafter` with no login shell.
- Sets up `/opt/mtga-metacrafter/` as the binary + data layout.
- Installs Caddy (Ubuntu package) and writes a minimal Caddyfile
  that proxies `:443` to our Go process on `127.0.0.1:8080`.
- Writes and enables a `mtga-metacrafter.service` systemd unit.
- *Doesn't* start the service yet — there's no binary on the box yet.

## 6. Domain (optional)

Caddy auto-fetches TLS from Let's Encrypt, but it needs a domain
name pointing to the public IP. If you want HTTPS:

1. Buy a cheap domain (Cloudflare Registrar, Namecheap, Porkbun:
   ~$10/year for `.com`, ~$2/year for `.xyz`).
2. Add an A record pointing the domain (e.g. `mtga-metacrafter.com`
   or `meta.francescolofranco.com`) to the public IP.
3. Edit `/etc/caddy/Caddyfile` on the VM and replace the
   `:443` placeholder with your domain. Caddy reloads on save.

If you don't want a domain, the service will still work over HTTP
on `http://<PUBLIC_IP>/`. Browsers will warn about no HTTPS but
HTMX still works fine.

## 7. First deploy

Two paths, pick one.

### 7a. Manual (one-off)

From the repo on your laptop:

```
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" \
  -o /tmp/mtga-metacrafter ./cmd/mtga-metacrafter
scp -i ~/.ssh/mtga-metacrafter /tmp/mtga-metacrafter \
  ubuntu@<PUBLIC_IP>:/tmp/
ssh -i ~/.ssh/mtga-metacrafter ubuntu@<PUBLIC_IP> "sudo install -o metacrafter -g metacrafter -m 755 /tmp/mtga-metacrafter /opt/mtga-metacrafter/mtga-metacrafter && sudo systemctl restart mtga-metacrafter"
```

Verify:

```
ssh -i ~/.ssh/mtga-metacrafter ubuntu@<PUBLIC_IP> "systemctl status mtga-metacrafter --no-pager | head -15"
curl http://<PUBLIC_IP>/healthz
```

### 7b. GitHub Actions

Add these repo secrets (Settings → Secrets → Actions):

- `ORACLE_HOST`: the public IP (or your domain if you set one up).
- `ORACLE_USER`: `ubuntu`.
- `ORACLE_SSH_KEY`: the **private** key contents (the whole
  `-----BEGIN ... PRIVATE KEY-----` block, including newlines).

Push to `main` and the `deploy-oracle.yml` workflow does the rest:
cross-compiles, scp's the binary, restarts the unit.

## 8. Tear-down (if needed)

If you ever want to delete the Oracle VM:

1. Console → Compute → Instances → click the instance → **Terminate**.
2. **Important**: also terminate the boot volume from the same dialog,
   otherwise it sticks around and counts against your free quota.

The DNS record (if you added one) needs to be removed separately at
your registrar.

## 9. Known annoyances

- **Idle-resource policy**: Oracle reserves the right to reclaim
  Always Free resources from accounts with "no activity". In
  practice this means a VM that does *something* (which ours does —
  a daily scrape) stays safe. A VM you spin up and forget for
  weeks may get reaped.
- **OS auto-updates**: Ubuntu unattended-upgrades is enabled by
  default and may briefly restart the VM. The systemd unit
  auto-restarts our service.
- **Free-tier capacity throttling**: at peak times Oracle may
  refuse `Create instance` for ARM shapes. Just retry every few
  hours.
