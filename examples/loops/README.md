# Loop examples

`document-maintenance.json` is the first v0.6 Loop template. It is read-only,
uses an argv-only verification check, and never asks GoHermit to commit, push,
open a pull request, merge, or deploy.

Import it from the Loop Workbench or with:

```bash
hermit loop import examples/loops/document-maintenance.json
```

After import, edit `workspace_identity` and select a company, access method,
and model that are already configured in this GoHermit installation. The
checked-in provider values are descriptive placeholders, not credentials.
Run Dry Run and resolve every readiness reason before starting an Invocation.
