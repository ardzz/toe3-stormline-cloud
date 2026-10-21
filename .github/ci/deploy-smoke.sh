#!/usr/bin/env bash
# Usage: .github/ci/deploy-smoke.sh caddy|traefik
# Builds the image, starts the compose stack from deploy/.env.example behind the chosen proxy with a test
# certificate, runs deploy/smoke.sh against it over HTTPS and removes everything again. It uses its own Compose
# projects and network (toe3-smoke*), and refuses to run where deploy/.env exists, so it never touches a deployment.
set -euo pipefail

proxy=${1:?usage: deploy-smoke.sh caddy|traefik}
root=$(cd "$(dirname "$0")/../.." && pwd)
deploy=$root/deploy
harness=$root/.github/ci/compose.traefik-test.yaml

case $proxy in
caddy) files=compose.yaml:compose.caddy.yaml ;;
traefik) files=compose.yaml:compose.traefik.yaml ;;
*) echo "unknown proxy $proxy" >&2 && exit 2 ;;
esac
if [ -e "$deploy/.env" ]; then
	echo "deploy-smoke: $deploy/.env exists; run this from a checkout that holds no deployment" >&2
	exit 2
fi

sed -e "s|^COMPOSE_FILE=.*|COMPOSE_FILE=$files|" \
	-e 's|^TOE3_DOMAIN=.*|TOE3_DOMAIN=localhost|' \
	-e 's|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=ci-password|' \
	-e 's|^TOE3_VERSION=.*|TOE3_VERSION=smoke|' \
	-e 's|^TOE3_REGISTRATION=.*|TOE3_REGISTRATION=open|' \
	-e 's|^#TOE3_HTTP_PORT=.*|TOE3_HTTP_PORT=8080|' \
	-e 's|^#TOE3_HTTPS_PORT=.*|TOE3_HTTPS_PORT=8443|' \
	-e 's|^#TRAEFIK_NETWORK=.*|TRAEFIK_NETWORK=toe3-smoke-edge|' \
	"$deploy/.env.example" >"$deploy/.env"

stack() { (cd "$deploy" && docker compose -p toe3-smoke "$@"); }
cleanup() {
	status=$?
	if [ "$status" != 0 ]; then
		stack logs --no-color || true
	fi
	stack down -v --remove-orphans || true
	if [ "$proxy" = traefik ]; then
		docker compose -f "$harness" down -v || true
	fi
	rm -f "$deploy/.env"
	exit "$status"
}
trap cleanup EXIT

if [ "$proxy" = traefik ]; then
	docker compose -f "$harness" up -d
fi
stack up -d --build --wait
CURL_OPTS=-k "$deploy/smoke.sh" https://localhost:8443
# The operator command from the README, run inside the distroless container.
printf 'correct horse battery\n' | stack exec -T app toe3-cloud user add operator-check | grep -q 'created user' || {
	echo "deploy-smoke: toe3-cloud user add failed inside the container" >&2
	exit 1
}
echo "deploy-smoke ok: $proxy"
