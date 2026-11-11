"""Check that task verify and Gatekeeper run the same example checks."""

import copy
from pathlib import Path
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[1]


def task_names(cmds):
    """Return the task: entries of a Task cmds list, in order."""
    return [c["task"] for c in cmds or [] if isinstance(c, dict) and "task" in c]


def taskfile_problems(taskfile):
    problems = []
    tasks = taskfile.get("tasks") or {}
    verify = task_names((tasks.get("verify") or {}).get("cmds"))
    if "examples:check" not in verify:
        problems.append("verify does not run examples:check")
    elif verify[0] != "examples:check":
        problems.append("verify does not run examples:check first")
    check = task_names((tasks.get("examples:check") or {}).get("cmds"))
    if check != ["build:go", "examples:validate", "examples:refs"]:
        problems.append(
            "examples:check does not run build:go, examples:validate, and examples:refs in order"
        )
    for name in ("examples:validate", "examples:refs"):
        if name not in tasks:
            problems.append(f"{name} task is missing")
    return problems


def step_runs(job):
    return [step.get("run") or "" for step in (job or {}).get("steps") or []]


def workflow_problems(workflow):
    problems = []
    jobs = workflow.get("jobs") or {}
    if not any("task examples:check" in run for run in step_runs(jobs.get("test"))):
        problems.append("test job does not run task examples:check")
    for name, job in jobs.items():
        for run in step_runs(job):
            if "find examples" in run:
                problems.append(f"{name} job has an inline find examples")
    return problems


def load(path):
    return yaml.safe_load((ROOT / path).read_text())


class ExampleCheckParityTests(unittest.TestCase):
    def setUp(self):
        self.taskfile = load("Taskfile.yml")
        self.workflow = load(".github/workflows/gatekeeper.yaml")

    def test_taskfile_runs_example_checks_in_verify(self):
        self.assertEqual(taskfile_problems(self.taskfile), [])

    def test_gatekeeper_runs_the_same_task(self):
        self.assertEqual(workflow_problems(self.workflow), [])

    def test_removing_examples_check_from_verify_is_caught(self):
        taskfile = copy.deepcopy(self.taskfile)
        cmds = taskfile["tasks"]["verify"]["cmds"]
        taskfile["tasks"]["verify"]["cmds"] = [
            c for c in cmds if not (isinstance(c, dict) and c.get("task") == "examples:check")
        ]
        self.assertIn("verify does not run examples:check", taskfile_problems(taskfile))

    def test_dropping_a_step_from_examples_check_is_caught(self):
        for missing in ("build:go", "examples:validate", "examples:refs"):
            with self.subTest(missing=missing):
                taskfile = copy.deepcopy(self.taskfile)
                cmds = taskfile["tasks"]["examples:check"]["cmds"]
                taskfile["tasks"]["examples:check"]["cmds"] = [
                    c for c in cmds if not (isinstance(c, dict) and c.get("task") == missing)
                ]
                self.assertNotEqual(taskfile_problems(taskfile), [])

    def test_removing_examples_check_from_workflow_is_caught(self):
        workflow = copy.deepcopy(self.workflow)
        workflow["jobs"]["test"]["steps"] = [
            s for s in workflow["jobs"]["test"]["steps"]
            if "task examples:check" not in (s.get("run") or "")
        ]
        self.assertIn("test job does not run task examples:check", workflow_problems(workflow))

    def test_inline_find_examples_is_caught(self):
        workflow = copy.deepcopy(self.workflow)
        workflow["jobs"]["test"]["steps"].append(
            {"name": "Validate example stacks", "run": 'find examples -name "*.yaml"'}
        )
        self.assertIn("test job has an inline find examples", workflow_problems(workflow))


if __name__ == "__main__":
    unittest.main()
