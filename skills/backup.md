---
name: ikuai-backup
description: Objects ikuai-cli saved before an update, toggle or delete, and how to restore them.
---

# Backup and restore

Routing `update`, `toggle` and `delete` save the current object first, to
`$IKUAI_CLI_BACKUP_DIR` or `~/.ikuai-cli/backups`. The path is printed on stderr.

```bash
ikuai-cli backup list --format json        # saved_at, api_path, id, file — newest first
ikuai-cli backup restore <FILE> --yes      # put the object back
ikuai-cli backup restore <FILE> --dry-run  # print the request only
```

- If the object still exists it is replaced; if it was deleted it is created again (the router
  assigns a new ID).
- An authentication or network error never triggers a re-create.
- `--dry-run` writes no backup: nothing is changed.
