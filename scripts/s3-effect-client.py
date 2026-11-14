#!/usr/bin/env python3
"""S3 synthetic effect-gateway client (writer side).

Signs one admission envelope with a session key issued by the gateway and posts it.
The session key is read from KC_EFFECT_KEY so it never appears in argv.

The signing payload must match internal/effectgateway/contract.go exactly:
length-prefixed fields joined by "|", with expires_at in RFC3339 whole seconds so
Go's RFC3339Nano round-trip is byte-identical.

--resolve pins the TCP destination while keeping TLS verification and SNI on the
real host name (for clients whose DNS does not resolve the tailnet FQDN).
"""
import hashlib
import hmac
import http.client
import json
import os
import socket
import ssl
import sys
import time
import urllib.parse

FIELDS = ("goal", "mutation", "provider", "operation", "action", "target", "plan", "grant",
          "generation", "session", "owner", "expires_at")


def signing_payload(req):
    return "".join(f"{len(req[f])}:{req[f]}|" for f in FIELDS).encode()


class PinnedHTTPSConnection(http.client.HTTPSConnection):
    """HTTPS to a pinned IP, verified against the real host name."""

    def __init__(self, host, port, ip, **kwargs):
        super().__init__(host, port, **kwargs)
        self._ip = ip

    def connect(self):
        sock = socket.create_connection((self._ip, self.port), self.timeout)
        self.sock = self._context.wrap_socket(sock, server_hostname=self.host)


def main(argv):
    import argparse

    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--url", required=True)
    p.add_argument("--goal", required=True)
    p.add_argument("--mutation", required=True)
    p.add_argument("--provider", default="cloudflare")
    p.add_argument("--operation", default="create-account-token")
    p.add_argument("--action", default="issue")
    p.add_argument("--target", required=True)
    p.add_argument("--plan", required=True)
    p.add_argument("--grant", required=True)
    p.add_argument("--owner", required=True)
    p.add_argument("--session", required=True)
    p.add_argument("--generation", type=int, required=True)
    p.add_argument("--ttl", type=int, default=60)
    p.add_argument("--resolve", default="", help="host:port:ip (pin TCP, keep SNI)")
    p.add_argument("--timeout", type=float, default=20.0)
    p.add_argument("--start-at", type=float, default=0.0,
                   help="sleep until this local epoch before sending (barrier)")
    args = p.parse_args(argv)

    key = os.environ.get("KC_EFFECT_KEY", "").strip()
    if len(key) < 64:
        print("KC_EFFECT_KEY missing or too short", file=sys.stderr)
        return 64

    expires = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + args.ttl))
    fields = {
        "goal": args.goal, "mutation": args.mutation, "provider": args.provider,
        "operation": args.operation, "action": args.action, "target": args.target, "plan": args.plan,
        "grant": args.grant, "generation": str(args.generation),
        "session": args.session, "owner": args.owner, "expires_at": expires,
    }
    signature = hmac.new(bytes.fromhex(key), signing_payload(fields), hashlib.sha256).hexdigest()
    body = json.dumps({
        "request": {
            "goal_id": fields["goal"], "mutation_id": fields["mutation"],
            "provider": fields["provider"], "operation": fields["operation"], "action": fields["action"],
            "target_digest": fields["target"], "plan_digest": fields["plan"],
            "grant_digest": fields["grant"], "generation": args.generation,
            "session": fields["session"], "owner": fields["owner"],
            "expires_at": expires,
        },
        "signature": signature,
    }).encode()

    parsed = urllib.parse.urlsplit(args.url)
    host, port = parsed.hostname, parsed.port or 443
    path = parsed.path or "/submit"
    context = ssl.create_default_context()
    headers = {"Content-Type": "application/json", "Content-Length": str(len(body))}

    try:
        if args.resolve:
            pinned_host, pinned_port, ip = args.resolve.split(":", 2)
            if pinned_host != host or int(pinned_port) != port:
                print("--resolve does not match the URL host/port", file=sys.stderr)
                return 64
            conn = PinnedHTTPSConnection(host, port, ip, context=context, timeout=args.timeout)
        else:
            conn = http.client.HTTPSConnection(host, port, context=context, timeout=args.timeout)
        if args.start_at:
            # Pre-establish TCP+TLS so the barrier releases into a warm socket.
            conn.connect()
            import time as _time
            _time.sleep(max(0.0, args.start_at - _time.time()))
        conn.request("POST", path, body=body, headers=headers)
        resp = conn.getresponse()
        text = resp.read().decode("utf-8", "replace").strip()
        conn.close()
        print(f"http={resp.status} {text}")
        return 0 if resp.status == 200 else 3
    except Exception as exc:
        print('{"status":"TRANSPORT_ERROR"}', f"{type(exc).__name__}: {exc}", sep="\n", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
