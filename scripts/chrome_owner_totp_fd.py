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


class OwnerFDTOTP:
    def __init__(self, conn: socket.socket, site: str, timeout_seconds: float = 1.0):
        if conn.family != socket.AF_UNIX or not isinstance(site, str) or not site:
            raise OwnerOTPDenied("INVALID_OWNER_SOCKET")
        if not 0 < timeout_seconds <= 2:
            raise OwnerOTPDenied("INVALID_TIMEOUT")
        self._conn = conn
        self._site = site
        self._timeout = timeout_seconds
        self._used = False

    def fill_after_owner_check(self, page, policy, request) -> None:
        if self._used:
            raise OwnerOTPDenied("ONE_SHOT_USED")
        # Mark used BEFORE reading: a partially consumed OTP cannot be retried.
        self._used = True
        if (getattr(request, "action", None) != "reauthenticate" or
            getattr(request, "site", None) != self._site or
            getattr(policy, "site", None) != self._site or
            getattr(policy, "mfa_mode", None) != "owner_totp"):
            raise OwnerOTPDenied("CHALLENGE_NOT_ADMITTED")
        url = urlsplit(getattr(page, "url", ""))
        if url.scheme != "https" or url.netloc != self._site or url.username is not None:
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
            code.extend(self._conn.recv(7))
            if len(code) != 6 or any(d < 48 or d > 57 for d in code):
                raise OwnerOTPDenied("BAD_OWNER_OTP_FRAME")
            # The sensitive code never becomes a model/CLI/log response.
            field.fill(code.decode("ascii"), timeout=2000)
            submit = getattr(policy, "otp_submit_selector", None)
            if submit:
                if len(submit) > 256 or page.locator(submit).count() != 1:
                    raise OwnerOTPDenied("OTP_SUBMIT_NOT_ADMITTED")
                page.locator(submit).click(timeout=2000)
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
