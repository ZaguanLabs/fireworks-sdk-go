#!/usr/bin/env python3
from __future__ import annotations

import ast
import importlib.util
import os
import re
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parent.parent
PYTHON_SDK_VERSION = "1.2.0-alpha.83"
DEFAULT_FIREWORKS_ROOT = REPO_ROOT / f"docs/fireworks-py/python-sdk-{PYTHON_SDK_VERSION}/src/fireworks"
FIREWORKS_ROOT = Path(os.environ.get("FIREWORKS_PY_ROOT", DEFAULT_FIREWORKS_ROOT)).expanduser()

RESOURCE_MODULES = {
    "accounts": "AccountsResource",
    "api_keys": "APIKeysResource",
    "batch_inference_jobs": "BatchInferenceJobsResource",
    "chat.completions": "ChatCompletionsResource",
    "completions": "CompletionsResource",
    "datasets": "DatasetsResource",
    "deployment_shape_versions": "DeploymentShapeVersionsResource",
    "deployment_shapes": "DeploymentShapesResource",
    "deployments": "DeploymentsResource",
    "dpo_jobs": "DPOJobsResource",
    "evaluation_jobs": "EvaluationJobsResource",
    "evaluators": "EvaluatorsResource",
    "lora": "LoraResource",
    "messages": "MessagesResource",
    "models": "ModelsResource",
    "reinforcement_fine_tuning_jobs": "ReinforcementFineTuningJobsResource",
    "reinforcement_fine_tuning_steps": "ReinforcementFineTuningStepsResource",
    "secrets": "SecretsResource",
    "supervised_fine_tuning_jobs": "SupervisedFineTuningJobsResource",
    "users": "UsersResource",
}

IGNORED_PY_METHODS = {"with_raw_response", "with_streaming_response"}


def require_root() -> None:
    if FIREWORKS_ROOT.is_dir():
        return
    raise SystemExit(
        f"Fireworks Python SDK source not found: {FIREWORKS_ROOT}\n"
        f"Set FIREWORKS_PY_ROOT=/path/to/python-sdk-{PYTHON_SDK_VERSION}/src/fireworks"
    )


def display_path(path: Path) -> str:
    try:
        return str(path.relative_to(REPO_ROOT))
    except ValueError:
        return str(path)


def pascal(name: str) -> str:
    special = {"api": "API", "dpo": "DPO", "id": "ID", "url": "URL", "rlor": "RLOR"}
    return "".join(special.get(part, part[:1].upper() + part[1:]) for part in name.split("_") if part)


def python_resource_methods() -> dict[str, set[str]]:
    out: dict[str, set[str]] = {}
    root = FIREWORKS_ROOT / "resources"
    for module, go_resource in RESOURCE_MODULES.items():
        path = root.joinpath(*module.split(".")).with_suffix(".py")
        tree = ast.parse(path.read_text())
        methods: set[str] = set()
        for node in tree.body:
            if not isinstance(node, ast.ClassDef) or node.name.startswith("Async"):
                continue
            for item in node.body:
                if isinstance(item, ast.FunctionDef) and not item.name.startswith("_") and item.name not in IGNORED_PY_METHODS:
                    methods.add(pascal(item.name))
        out[go_resource] = methods
    return out


def go_resource_methods() -> dict[str, set[str]]:
    out: dict[str, set[str]] = {}
    for filename in ("resources.go", "resource_typed.go"):
        text = (REPO_ROOT / filename).read_text()
        for match in re.finditer(r"func \(r \*([A-Za-z0-9]+Resource)\) ([A-Z][A-Za-z0-9]+)\(", text):
            out.setdefault(match.group(1), set()).add(match.group(2).removesuffix("Typed"))
    return out


def expected_type_names() -> set[str]:
    spec = importlib.util.spec_from_file_location("generate_types", REPO_ROOT / "scripts/generate_types.py")
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["generate_types"] = module
    spec.loader.exec_module(module)
    refs = module.collect_type_refs()
    return {ref.go_name for ref in refs.values()} | top_level_type_exports()


def top_level_type_exports() -> set[str]:
    init_file = FIREWORKS_ROOT / "types" / "__init__.py"
    tree = ast.parse(init_file.read_text())
    out: set[str] = set()
    for node in tree.body:
        if not isinstance(node, ast.ImportFrom) or node.module == "__future__":
            continue
        for alias in node.names:
            out.add(alias.asname or alias.name)
    return out


def go_type_names() -> set[str]:
    text = (REPO_ROOT / "types/generated.go").read_text() + "\n" + (REPO_ROOT / "types/aliases.go").read_text()
    return set(re.findall(r"^type ([A-Z][A-Za-z0-9]+) ", text, re.MULTILINE))


def main() -> None:
    require_root()
    py_resources = python_resource_methods()
    go_resources = go_resource_methods()
    expected_types = expected_type_names()
    actual_types = go_type_names()

    print(f"# Fireworks SDK Go resource/type parity report ({PYTHON_SDK_VERSION})\n")
    print(f"Python SDK source: `{display_path(FIREWORKS_ROOT)}`.\n")
    print("## Resource Methods\n")
    print("| Resource | Python methods | Missing in Go | Extra Go methods |")
    print("| --- | ---: | --- | --- |")
    for resource in sorted(py_resources):
        py_methods = py_resources[resource]
        go_methods = go_resources.get(resource, set())
        missing = sorted(py_methods - go_methods)
        extra = sorted(method for method in go_methods - py_methods if not method.endswith("Stream"))
        print(
            f"| `{resource}` | {len(py_methods)} | "
            f"{', '.join(missing) if missing else 'none'} | "
            f"{', '.join(extra) if extra else 'none'} |"
        )

    missing_types = sorted(expected_types - actual_types)
    print("\n## Type Catalog\n")
    print(f"- Python classes, exported aliases, and top-level type exports expected: {len(expected_types)}")
    print(f"- Go generated/alias type names: {len(actual_types)}")
    print(f"- Missing expected Go names: {', '.join(missing_types) if missing_types else 'none'}")
    print("- Extra Go names are helper aliases and pagination wrappers.")


if __name__ == "__main__":
    main()
