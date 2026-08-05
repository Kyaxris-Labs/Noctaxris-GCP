<!-- Short description (max 100 chars):
Run GCP-shaped security labs without a cloud bill or a host Docker socket.
-->

<p align="center">
  <img src="https://raw.githubusercontent.com/Kyaxris-Labs/Noctaxris-GCP/main/assets/noctaxris_gcp_bg.png" alt="Noctaxris-GCP" width="640">
</p>

<p align="center">
  <b>Run GCP-shaped security labs on your laptop without a cloud bill or a host Docker socket.</b>
</p>

```bash
docker pull kyaxris/noctaxris-gcp:latest
# Generate unique roots (shipped example pair is refused).
ROOT_SA="root@$(openssl rand -hex 4).iam.gserviceaccount.com"
ROOT_TOKEN="$(openssl rand -hex 32)"
docker run -d --name noctaxris-gcp -p 127.0.0.1:4588:4588 \
  -e NOCTAXRIS_GCP_LISTEN=0.0.0.0:4588 \
  -e NOCTAXRIS_GCP_ALLOW_NONLOOPBACK_LISTEN=1 \
  -e NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT="$ROOT_SA" \
  -e NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN="$ROOT_TOKEN" \
  kyaxris/noctaxris-gcp:latest
curl http://127.0.0.1:4588/_noctaxris-gcp/health
```

Point GCP clients at `http://127.0.0.1:4588` with `Authorization: Bearer <token>`. Tags: `latest`, semver, `nightly`.

Full service matrix and docs: [github.com/Kyaxris-Labs/Noctaxris-GCP](https://github.com/Kyaxris-Labs/Noctaxris-GCP).

License: [MIT](https://github.com/Kyaxris-Labs/Noctaxris-GCP/blob/main/LICENSE)
