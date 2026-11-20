"""Kernel-backed local peer check for a future Mac Chrome owner broker.

This is a *preflight*: it never grants browser execution or supplies a secret.
Actual browser effects also require original Goal/Grant/Claim + owner GUI tests.
A mere same-UID Unix socket is NOT an OS security boundary.
"""
from __future__ import annotations

import ctypes
import os
import socket
import sys
from dataclasses import dataclass


@dataclass(frozen=True)
class PeerDecision:
    status: str
    reason: str
    authorizes_execution: bool = False


class BoundaryDenied(RuntimeError):
    pass


def macos_peer_ids(conn: socket.socket) -> tuple[int, int]:
    """Read actual kernel peer credentials; never accept a caller UID field."""
    if sys.platform != "darwin" or conn.family != socket.AF_UNIX or conn.type & socket.SOCK_STREAM != socket.SOCK_STREAM:
        raise BoundaryDenied("OWNER_MAC_UNIX_REQUIRED")
    if hasattr(conn, "getpeereid"):
        try:
            return conn.getpeereid()
        except OSError:
            raise BoundaryDenied("PEER_ID_UNAVAILABLE") from None
    # macOS libc's getpeereid(3) is authoritative for a connected AF_UNIX stream.
    try:
        libc = ctypes.CDLL("/usr/lib/libSystem.B.dylib", use_errno=True)
        getpeereid = libc.getpeereid
        getpeereid.argtypes = [ctypes.c_int, ctypes.POINTER(ctypes.c_uint), ctypes.POINTER(ctypes.c_uint)]
        getpeereid.restype = ctypes.c_int
        uid, gid = ctypes.c_uint(), ctypes.c_uint()
        if getpeereid(conn.fileno(), ctypes.byref(uid), ctypes.byref(gid)) != 0:
            raise BoundaryDenied("PEER_ID_UNAVAILABLE")
        return uid.value, gid.value
    except (OSError, AttributeError, ValueError):
        raise BoundaryDenied("PEER_ID_UNAVAILABLE") from None


def evaluate_peer(owner_uid: int, agent_uid: int, peer_uid: int | None,
                  *, owner_console_uid: int | None, owner_is_gui: bool) -> PeerDecision:
    """Reject same-UID Mac agent and unverified/privileged peers.

    Inputs must originate from the owner supervisor / getpeereid, NOT a model or
    the network. Even a matching peer is only a HOLD until signed original Grant.
    """
    if any(type(v) is not int or v < 1 for v in (owner_uid, agent_uid)):
        return PeerDecision("BLOCKED", "INVALID_OS_IDENTITY")
    if agent_uid == owner_uid:
        return PeerDecision("BLOCKED", "SAME_UID_AGENT_UNISOLATED")
    if type(peer_uid) is not int or peer_uid != agent_uid:
        return PeerDecision("BLOCKED", "PEER_UID_MISMATCH")
    if owner_console_uid != owner_uid or owner_is_gui is not True:
        return PeerDecision("BLOCKED", "OWNER_GUI_UNVERIFIED")
    return PeerDecision("HOLD", "KERNEL_PEER_ONLY_ORIGINAL_GRANT_REQUIRED")


def inspect_owner_socket(conn: socket.socket, *, expected_agent_uid: int,
                         owner_console_uid: int, owner_is_gui: bool) -> PeerDecision:
    """Read-only owner process check; cannot perform, authorize or receive OTP."""
    if sys.platform != "darwin":
        return PeerDecision("BLOCKED", "OWNER_MAC_REQUIRED")
    try:
        peer_uid, _gid = macos_peer_ids(conn)
    except BoundaryDenied:
        return PeerDecision("BLOCKED", "PEER_ID_UNAVAILABLE")
    return evaluate_peer(os.geteuid(), expected_agent_uid, peer_uid,
                         owner_console_uid=owner_console_uid, owner_is_gui=owner_is_gui)
