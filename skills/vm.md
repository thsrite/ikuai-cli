---
name: ikuai-vm
description: iKuai virtual machines — list, start, stop (web management API).
---

# Virtual machines

The local REST API has no VM endpoints; these commands use the web management API.

```bash
export IKUAI_CLI_WEB_USER=admin IKUAI_CLI_WEB_PASSWORD=<password>
ikuai-cli vm list --format json            # id, name, state, cpu_cores, mem_size, auto_start, vnc_port
ikuai-cli vm start <NAME> --format json
ikuai-cli vm stop <NAME> --yes --format json   # powers the VM off
ikuai-cli vm stop <NAME> --dry-run             # print the call only
```

- Starting a running VM or stopping a stopped one does nothing.
- Changing CPU or memory needs the VM powered off; rebooting the router restarts every VM
  (services on them go down with it).
- Do not rely on the web UI's VM snapshots: on 4.0.308 they report success but none are kept,
  and the VM pauses for several seconds.
