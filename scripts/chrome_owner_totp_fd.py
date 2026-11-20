"""Mac owner Chrome sink for one TOTP from a trusted inherited socketpair.

This does not generate or export OTP. Go owner2fa.Fulfill (separate PR #83)
performs an independent MFA child reservation before sending six bytes.
Only a privileged browser supervisor may construct this object; never use
an agent-provided descriptor, path or socket and never expose Chrome CDP.
"""
from __future__ import annotations

import socket
from urllib.parse import urlsplit


class OwnerOTPDenied(ValueError):
    pass


def _same_https_origin(page, site: str) -> bool:
    # Re-check the full secure origin before acknowledging field input.
    # A matching host after a redirect to HTTP is not the same origin.
    try:
        url = urlsplit(getattr(page, "url", ""))
        return (url.scheme == "https" and url.netloc == site and
                url.username is None and url.password is None)
    except (TypeError, ValueError):
        return False


class OwnerFDTOTP:
    def __init__(self, conn: socket.socket, site: str,
                 parent_request_id: str, child_request_id: str,
                 timeout_seconds: float = 1.0):
        if conn.family != socket.AF_UNIX or not isinstance(site, str) or not site:
            raise OwnerOTPDenied("INVALID_OWNER_SOCKET")
        def valid_id(value):
            return (isinstance(value, str) and 8 <= len(value) <= 128 and
                    all(c.isascii() and (c.isalnum() or c in "_-") for c in value))
        if (not valid_id(parent_request_id) or not valid_id(child_request_id) or
                parent_request_id == child_request_id):
            raise OwnerOTPDenied("INVALID_MFA_CHILD_BINDING")
        if not 0 < timeout_seconds <= 2:
            raise OwnerOTPDenied("INVALID_TIMEOUT")
        self._conn = conn
        self._site = site
        self._parent_request_id = parent_request_id
        self._child_request_id = child_request_id
        self._timeout = timeout_seconds
        self._used = False

    def fill_after_owner_check(self, page, policy, request) -> None:
        if self._used:
            raise OwnerOTPDenied("ONE_SHOT_USED")
        # Mark used BEFORE reading: a partially consumed OTP cannot be retried.
        self._used = True
        if (getattr(request, "action", None) != "reauthenticate" or
            getattr(request, "site", None) != self._site or
            getattr(request, "request_id", None) != self._parent_request_id or
            getattr(policy, "site", None) != self._site or
            getattr(policy, "mfa_mode", None) != "owner_totp"):
            raise OwnerOTPDenied("CHALLENGE_NOT_ADMITTED")
        if not _same_https_origin(page, self._site):
            raise OwnerOTPDenied("CROSS_ORIGIN")
        selector = getattr(policy, "otp_selector", None)
        if not selector or len(selector) > 256:
            raise OwnerOTPDenied("OTP_FIELD_NOT_ADMITTED")
        field = page.locator(selector)
        if field.count() != 1:
            raise OwnerOTPDenied("OTP_FIELD_NOT_ADMITTED")
        code = bytearray()
        try:
            self._conn.settimeout(self._timeout)
            while len(code) < 6:
                part = self._conn.recv(6 - len(code))
                if not part:
                    raise OwnerOTPDenied("BAD_OWNER_OTP_FRAME")
                code.extend(part)
            if any(d < 48 or d > 57 for d in code):
                raise OwnerOTPDenied("BAD_OWNER_OTP_FRAME")
            # The sensitive code never becomes a model/CLI/log response.
            field.fill(code.decode("ascii"), timeout=2000)
            if not _same_https_origin(page, self._site):
                raise OwnerOTPDenied("CROSS_ORIGIN")
            submit = getattr(policy, "otp_submit_selector", None)
            if submit:
                if len(submit) > 256 or page.locator(submit).count() != 1:
                    raise OwnerOTPDenied("OTP_SUBMIT_NOT_ADMITTED")
                page.locator(submit).click(timeout=2000)
            if not _same_https_origin(page, self._site):
                raise OwnerOTPDenied("CROSS_ORIGIN")
            # This ACK proves input action completion, NOT site authentication.
            # Go must fail closed if ACK never arrives; uncertain effects are
            # not retried automatically.
            self._conn.sendall(b"\x01")
        except OwnerOTPDenied:
            raise
        except Exception:
            raise OwnerOTPDenied("OTP_OWNER_IO_FAILED") from None
        finally:
            for i in range(len(code)):
                code[i] = 0
