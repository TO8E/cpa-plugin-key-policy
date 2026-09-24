"""Load the Linux plugin via its real C ABI; use synthetic data, no upstream calls."""

import base64
import ctypes as ct
import hashlib
import json
from pathlib import Path
import sys
import tempfile


class Buffer(ct.Structure):
    _fields_ = [("ptr", ct.c_void_p), ("len", ct.c_size_t)]


Call = ct.CFUNCTYPE(ct.c_int, ct.c_char_p, ct.c_void_p, ct.c_size_t, ct.POINTER(Buffer))
Free = ct.CFUNCTYPE(None, ct.c_void_p, ct.c_size_t)
Shutdown = ct.CFUNCTYPE(None)


class API(ct.Structure):
    _fields_ = [("abi_version", ct.c_uint32), ("call", Call), ("free", Free), ("shutdown", Shutdown)]


lib = ct.CDLL(str(Path(sys.argv[1]).resolve()))
lib.cliproxy_plugin_init.argtypes = [ct.c_void_p, ct.POINTER(API)]
lib.cliproxy_plugin_init.restype = ct.c_int
api = API()
assert lib.cliproxy_plugin_init(None, ct.byref(api)) == 0
assert api.abi_version == 1


def call(method, request):
    raw = json.dumps(request).encode()
    source = ct.create_string_buffer(raw)
    response = Buffer()
    code = api.call(method.encode(), source, len(raw), ct.byref(response))
    try:
        assert code == 0, f"{method}: C ABI returned {code}"
        return json.loads(ct.string_at(response.ptr, response.len))
    finally:
        if response.ptr:
            api.free(response.ptr, response.len)


with tempfile.TemporaryDirectory() as directory:
    try:
        keys = ["cpa_smoke_a", "cpa_smoke_b"]
        config = {
            "enabled": True,
            "state_file": str(Path(directory) / "state.json"),
            "keys": [{
                "id": key,
                "enabled": True,
                "key_hash": "sha256:" + hashlib.sha256(key.encode()).hexdigest(),
                "models": [{"alias": "fast", "provider": "codex", "target_model": "gpt-5-codex"}],
            } for key in keys],
        }
        registered = call("plugin.register", {
            "config_yaml": base64.b64encode(json.dumps(config).encode()).decode(),
        })
        assert registered["ok"], registered
        assert registered["result"]["metadata"]["Version"] == "0.5.2-to8e.1", registered

        # Both existing and newly issued keys must use recovered candidates.
        for key in keys:
            headers = {"Authorization": ["Bearer " + key]}
            authenticated = call("frontend_auth.authenticate", {
                "Method": "POST", "Path": "/v1/chat/completions", "Headers": headers,
                "Body": base64.b64encode(b'{"model":"fast"}').decode(),
            })
            assert authenticated["ok"] and authenticated["result"]["Authenticated"], authenticated
            picked = call("scheduler.pick", {
                "Provider": "codex", "Model": "gpt-5-codex",
                "Options": {"Headers": headers},
                "Candidates": [{"ID": "recovered-auth", "Provider": "codex", "Status": "error"}],
            })
            assert picked["ok"] and picked["result"]["AuthID"] == "recovered-auth", picked

        resource = call("management.handle", {
            "Method": "GET", "Path": "/v0/resource/plugins/cpa-key-policy/index.html",
        })
        assert resource["ok"] and resource["result"]["StatusCode"] == 200, resource
        html = base64.b64decode(resource["result"]["Body"])
        assert len(html) > 100_000 and b"<script" in html, "Expected the built management UI"
        print("C ABI smoke passed: registration, two synthetic keys, recovered auth scheduling, embedded UI")
    finally:
        api.shutdown()
