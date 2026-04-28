from __future__ import annotations

import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from openmate_agent.skill_catalog import SKILL_DESCRIPTION_TRUNCATE, SKILL_QUERY_THRESHOLD, SkillCatalog


class SkillCatalogTests(unittest.TestCase):
    def test_scan_merge_dedupe_and_skip_invalid(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            project_skill = root / ".skill" / "alpha" / "SKILL.md"
            project_skill.parent.mkdir(parents=True, exist_ok=True)
            project_skill.write_text("# alpha\nalpha desc\n", encoding="utf-8")
            dup = root / ".skill" / "alpha_dup" / "SKILL.md"
            dup.parent.mkdir(parents=True, exist_ok=True)
            dup.write_text("# alpha\nanother desc\n", encoding="utf-8")
            invalid = root / ".skill" / "invalid" / "SKILL.md"
            invalid.parent.mkdir(parents=True, exist_ok=True)
            invalid.write_text("# invalid\n", encoding="utf-8")

            ext = root / "custom-skill"
            beta = ext / "beta" / "SKILL.md"
            beta.parent.mkdir(parents=True, exist_ok=True)
            beta.write_text("# beta\nbeta desc\n", encoding="utf-8")

            catalog = SkillCatalog(workspace_root=root)
            skills = catalog.scan(extra_path=str(ext))
            names = [item.name for item in skills]
            self.assertEqual(names, ["alpha", "beta"])

    def test_query_threshold_truncates_description(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            skill_root = root / ".skill"
            for i in range(SKILL_QUERY_THRESHOLD + 1):
                path = skill_root / f"s{i}" / "SKILL.md"
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(f"# skill_{i}\n" + ("x" * 40) + "\n", encoding="utf-8")
            catalog = SkillCatalog(workspace_root=root)
            result = catalog.query()
            self.assertEqual(result.mode, "truncated")
            self.assertEqual(result.remaining_count, SKILL_QUERY_THRESHOLD + 1)
            self.assertTrue(all(len(item.description) == SKILL_DESCRIPTION_TRUNCATE for item in result.skills))


if __name__ == "__main__":
    unittest.main()
