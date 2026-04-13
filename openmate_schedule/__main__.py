"""Module entrypoint for `python -m openmate_schedule`."""

from .cli import main


if __name__ == "__main__":
    raise SystemExit(main())
