import socket
import threading
import time
import unittest
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from types import SimpleNamespace
from chrome_owner_totp_fd import OwnerFDTOTP, OwnerOTPDenied

class Field:
    def __init__(self, count=1): self.value = None; self.clicks=0; self.n=count
    def count(self): return self.n
    def fill(self, value, **kwargs): self.value=value
    def click(self, **kwargs): self.clicks+=1
class Page:
    def __init__(self, url="https://example.com/mfa"):
        self.url=url;self.otp=Field();self.submit=Field()
    def locator(self, selector): return self.otp if selector=="#otp" else self.submit

def data():
    return (SimpleNamespace(site="example.com",mfa_mode="owner_totp",otp_selector="#otp",otp_submit_selector="#submit"),
            SimpleNamespace(site="example.com",action="reauthenticate",request_id="parent-0001"))
class TestOwnerFDTOTP(unittest.TestCase):
    def pair(self, raw=b"123456"):
        a,b=socket.socketpair()
        self.addCleanup(a.close);self.addCleanup(b.close)
        a.sendall(raw)
        self._sender = a
        return b
    def test_synthetic_otp_fills_only_owner_browser(self):
        conn=self.pair(); p=Page();policy,req=data()
        OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        self.assertEqual(p.otp.value,"123456")
        self.assertEqual(p.submit.clicks,1)
        self.assertEqual(self._sender.recv(1), b"\x01")
    def test_no_replay(self):
        conn=self.pair();p=Page();policy,req=data();adapter=OwnerFDTOTP(conn,"example.com","parent-0001","child-0001")
        adapter.fill_after_owner_check(p,policy,req)
        with self.assertRaisesRegex(OwnerOTPDenied,"ONE_SHOT_USED"):
            adapter.fill_after_owner_check(p,policy,req)
    def test_wrong_site_never_consumes_otp(self):
        conn=self.pair();p=Page(url="https://evil.test/mfa");policy,req=data();adapter=OwnerFDTOTP(conn,"example.com","parent-0001","child-0001")
        with self.assertRaisesRegex(OwnerOTPDenied,"CROSS_ORIGIN"):
            adapter.fill_after_owner_check(p,policy,req)
        self.assertIsNone(p.otp.value)
    def test_manual_apple_mode_never_fetches_code(self):
        conn=self.pair();p=Page();policy,req=data();policy.mfa_mode="apple_extension"
        with self.assertRaisesRegex(OwnerOTPDenied,"CHALLENGE_NOT_ADMITTED"):
            OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        self.assertIsNone(p.otp.value)
    def test_bad_frame_does_not_leak(self):
        conn=self.pair(b"badotp");p=Page();policy,req=data()
        with self.assertRaises(OwnerOTPDenied) as ex:
            OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        self.assertNotIn("badotp",str(ex.exception))
        self.assertIsNone(p.otp.value)
    def test_missing_or_ambiguous_selector(self):
        conn=self.pair();p=Page();policy,req=data();p.otp.n=2
        with self.assertRaisesRegex(OwnerOTPDenied,"OTP_FIELD_NOT_ADMITTED"):
            OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
    def test_mfa_child_binding_mismatch_never_consumes_code(self):
        conn=self.pair();p=Page();policy,req=data();req.request_id="other-parent"
        with self.assertRaisesRegex(OwnerOTPDenied,"CHALLENGE_NOT_ADMITTED"):
            OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        self.assertIsNone(p.otp.value)
        with self.assertRaisesRegex(OwnerOTPDenied,"INVALID_MFA_CHILD_BINDING"):
            OwnerFDTOTP(conn,"example.com","parent-0001","parent-0001")
    def test_fragmented_six_byte_socket_frame(self):
        sender,conn=socket.socketpair();self.addCleanup(sender.close);self.addCleanup(conn.close)
        p=Page();policy,req=data()
        def send():
            sender.sendall(b"12")
            time.sleep(0.05)
            sender.sendall(b"3456")
        thread=threading.Thread(target=send)
        thread.start()
        OwnerFDTOTP(conn,"example.com","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        thread.join(1)
        self.assertEqual(p.otp.value,"123456")
        self.assertEqual(sender.recv(1),b"\x01")
    def test_no_real_network_or_credential_store(self):
        conn=self.pair();policy,req=data();p=Page()
        with self.assertRaises(OwnerOTPDenied):
            OwnerFDTOTP(conn,"wrong.test","parent-0001","child-0001").fill_after_owner_check(p,policy,req)
        self.assertIsNone(p.otp.value)

if __name__=="__main__": unittest.main()
