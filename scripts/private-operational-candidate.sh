#!/usr/bin/env bash
set -euo pipefail
umask 077

control="$PWD/release-control"
source="$PWD/source"
candidate="$PWD/candidate"
image=sha256:3394b74b17b9b3cb6f3ce8ac19ad7df001fb6d473990e403435a255bf81059bd

test "$(git -C "$control" rev-parse HEAD)" = "$WORKFLOW_SHA"
test "$(git -C "$source" rev-parse HEAD)" = "$SOURCE_SHA"
test "$(git -C "$source" rev-parse 'HEAD^{tree}')" = "$SOURCE_TREE"
test -z "$(git -C "$source" status --porcelain)"
APPROVAL_REF="keycli-g4b/$(sha256sum "$control/.github/candidate/producer.pub" | cut -d' ' -f1)"
mkdir -m 700 "$candidate"

podman run --rm --pull=never --userns=keep-id --user=1001:1001 \
  --network=none --pid=private --ipc=private --uts=private --cap-drop=ALL \
  --security-opt=no-new-privileges --read-only --read-only-tmpfs=false \
  --image-volume=ignore --http-proxy=false --no-hosts --unsetenv-all --umask=0077 \
  --tmpfs /tmp:rw,exec,nosuid,nodev,mode=1777 \
  --volume "$source:/work/source:ro,rprivate" \
  --volume "$control:/work/control:ro,rprivate" \
  --volume "$candidate:/work/candidate:rw,rprivate" \
  --volume /opt/keycli-go-modcache:/opt/keycli-go-modcache:ro,rprivate \
  --env PATH=/usr/local/go/bin:/usr/bin:/bin --env HOME=/tmp/home --env TMPDIR=/tmp \
  --env GOMODCACHE=/opt/keycli-go-modcache --env GOCACHE=/tmp/go-cache \
  --env GOPROXY=off --env GOSUMDB=off --env GOWORK=off --env GOTOOLCHAIN=local \
  --env GOFLAGS=-mod=readonly --env CGO_ENABLED=0 \
  --env SOURCE_SHA="$SOURCE_SHA" --env SOURCE_TREE="$SOURCE_TREE" \
  --env RELEASE_VERSION="$RELEASE_VERSION" --env RELEASE_CHANNEL="$RELEASE_CHANNEL" \
  --env RUNNER_MODE="$RUNNER_MODE" --env APPROVAL_REF="$APPROVAL_REF" \
  --env TAG_OBJECT= --env WORKFLOW_SHA="$WORKFLOW_SHA" \
  --env GITHUB_REPOSITORY="$GITHUB_REPOSITORY" --env GITHUB_RUN_ID="$GITHUB_RUN_ID" \
  --env GITHUB_RUN_ATTEMPT="$GITHUB_RUN_ATTEMPT" \
  --entrypoint /bin/bash "$image" -euo pipefail -c '
    mkdir -m 700 "$HOME"
    cp -a /work/source /tmp/source
    cd /tmp/source
    git show -s --format="%H %T"
    test "$(git rev-parse HEAD)" = "$SOURCE_SHA"
    test "$(git rev-parse "HEAD^{tree}")" = "$SOURCE_TREE"
    for suite in docs policy unit; do
      bash verify.sh --suite "$suite"
    done
    KC_VERSION="$RELEASE_VERSION" KC_COMMIT="$SOURCE_SHA" KC_BUILD_CHANNEL="$RELEASE_CHANNEL" \
      bash scripts/lifecycle.sh release
    contract=/work/control/scripts/release-contract.sh
    bash "$contract" provenance > dist/provenance.json
    (cd dist && sha256sum provenance.json >> SHA256SUMS)
    MANIFEST_SHA="$(sha256sum dist/SHA256SUMS | cut -d" " -f1)" \
      bash "$contract" verify-bundle
    bash "$contract" verify-binary arm64
    cp dist/* /work/candidate/
  '
sha256sum "$candidate/SHA256SUMS"
