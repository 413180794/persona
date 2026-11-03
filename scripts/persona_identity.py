#!/usr/bin/env python3
"""Resolve runtime state roots without reading build configuration or credentials."""
import argparse
import os
import pathlib
import plistlib
import shlex


def env_value(name, environment=None):
    environment = os.environ if environment is None else environment
    suffix = name.split("_", 1)[1] if name.startswith(("PERSONA_", "JARVIS_")) else name
    return environment.get("PERSONA_" + suffix, environment.get("JARVIS_" + suffix, ""))


def data_dir(home=None, environment=None):
    home = os.path.expanduser("~") if home is None else os.fspath(home)
    root = env_value("PERSONA_HOME", environment)
    if root:
        if root == "~": root = home
        elif root.startswith("~/"): root = os.path.join(home, root[2:])
        elif not os.path.isabs(root): root = os.path.join(home, root)
        return os.path.normpath(root)
    current, legacy = os.path.join(home, ".persona"), os.path.join(home, ".jarvis")
    if os.path.lexists(legacy):
        try:
            if os.path.samefile(current, legacy): return current
        except OSError:
            pass
        return legacy
    return current


def _alias(home, root, alias, environment):
    try:
        os.symlink(os.path.basename(root), alias)
    except FileExistsError:
        pass
    except OSError:
        if os.path.isdir(data_dir(home, environment)):
            return
        raise
    if not os.path.samefile(root, alias):
        raise OSError("state directories changed during initialization; retry without moving either directory")


def prepare_dir(home=None, environment=None):
    home = os.path.expanduser("~") if home is None else os.fspath(home)
    root = data_dir(home, environment)
    current, legacy = os.path.join(home, ".persona"), os.path.join(home, ".jarvis")
    if not os.path.lexists(current) and (not os.path.lexists(legacy) or not os.path.islink(legacy) or os.path.isdir(legacy)) and root in (current, legacy):
        os.makedirs(legacy, mode=0o700, exist_ok=True)
        _alias(home, legacy, current, environment)
        return data_dir(home, environment)
    if os.path.islink(root) and not os.path.isdir(root):
        raw = os.readlink(root)
        if not os.path.isabs(raw):
            raw = os.path.dirname(root) + "/" + raw
        while raw.endswith("/") or raw.endswith("/."):
            raw = raw[:-2] if raw.endswith("/.") else raw[:-1]
        target, parent = os.path.normpath(raw), os.path.dirname(raw)
        if root not in (current, legacy) or target not in (current, legacy) or not os.path.isdir(parent) or os.path.realpath(parent) != os.path.realpath(home) or os.path.lexists(target):
            raise OSError("state symlink target is unavailable; restore it before upgrading")
        os.makedirs(target, mode=0o700, exist_ok=True)
    os.makedirs(root, mode=0o700, exist_ok=True)
    if root in (current, legacy):
        alias = legacy if root == current else current
        if not os.path.lexists(alias):
            _alias(home, root, alias, environment)
    return data_dir(home, environment)


def _unit_environment(unit, home):
    values = {}
    files = [unit, *sorted(unit.with_name(unit.name + ".d").glob("*.conf"))]
    for file in files:
        try:
            lines = file.read_text().splitlines()
        except FileNotFoundError:
            continue
        section, continued = "", ""
        for line in lines:
            line = line.strip()
            if not line or line.startswith(("#", ";")):
                continue
            line = continued + line
            if line.endswith("\\"):
                continued = line[:-1] + " "
                continue
            continued = ""
            if line.startswith("["):
                section = line
                continue
            if section != "[Service]":
                continue
            setting, sep, text = line.partition("=")
            if not sep:
                continue
            if setting in ("EnvironmentFile", "UnsetEnvironment") and text:
                raise OSError("service environment uses external or removed values; set PERSONA_HOME explicitly before deployment")
            if setting != "Environment":
                continue
            if not text:
                values.clear()
                continue
            try:
                entries = shlex.split(text)
            except ValueError as error:
                raise OSError("service environment could not be parsed; set PERSONA_HOME explicitly") from error
            if not entries:
                values.clear()
            for entry in entries:
                key, sep, value = entry.partition("=")
                if sep and key in ("PERSONA_HOME", "JARVIS_HOME"):
                    values[key] = value.replace("%%", "\0").replace("%h", home).replace("\0", "%")
    return values


def service_environment(home=None, environment=None, role="auto"):
    """Recover only the requested service's root; never borrow a different service's profile."""
    environment = dict(os.environ if environment is None else environment)
    if "PERSONA_HOME" in environment or "JARVIS_HOME" in environment:
        return environment
    home = os.path.expanduser("~") if home is None else os.fspath(home)
    config = pathlib.Path(environment.get("XDG_CONFIG_HOME") or os.path.join(home, ".config"))
    units = {
        "hub": ("jarvisd.service",),
        "node": ("jarvisd-node.service", "jarvis-link.service"),
        "link": ("jarvis-link.service", "jarvisd-node.service"),
        "auto": ("jarvisd.service", "jarvisd-node.service", "jarvis-link.service"),
    }[role]
    found = []
    for name in units:
        unit = config / "systemd/user" / name
        installed = unit.exists() or unit.with_name(unit.name + ".d").is_dir()
        values = _unit_environment(unit, home)
        if installed:
            if role != "auto":
                return dict(environment, **values)
            found.append(values)
    if role != "hub":
        names = ("com.jarvis.link.plist", "com.jarvis.node.plist") if role == "link" else ("com.jarvis.node.plist", "com.jarvis.link.plist")
        for name in names:
            try:
                stored = plistlib.loads(pathlib.Path(home, "Library/LaunchAgents", name).read_bytes())
            except FileNotFoundError:
                continue
            values = stored.get("EnvironmentVariables", {})
            values = {key: values[key] for key in ("PERSONA_HOME", "JARVIS_HOME") if isinstance(values.get(key), str)}
            if role != "auto":
                return dict(environment, **values)
            found.append(values)
    if found:
        roots = [data_dir(home, values) for values in found]
        for root in roots[1:]:
            try:
                same = root == roots[0] or os.path.samefile(root, roots[0])
            except OSError:
                same = False
            if not same:
                raise OSError("installed services use different state roots; choose --service hub, node or link")
        environment.update(found[0])
    return environment


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prepare", action="store_true")
    parser.add_argument("--service", nargs="?", const="auto", choices=("auto", "hub", "node", "link"))
    parser.add_argument("--prefix", metavar="SCRIPT")
    args = parser.parse_args()
    if args.prefix:
        code = pathlib.Path(__file__).read_text()
        print("_PERSONA_STATE_DIR=$(python3 - --service " + (args.service or "auto") + (" --prepare" if args.prepare else "") + " <<'PERSONA_STATE_RESOLVER'\n" + code + "\nPERSONA_STATE_RESOLVER\n) || exit $?")
        print('export PERSONA_HOME="$_PERSONA_STATE_DIR" JARVIS_HOME="$_PERSONA_STATE_DIR"')
        if args.prefix != "-": print(pathlib.Path(args.prefix).read_text(), end="")
        return
    environment = service_environment(role=args.service) if args.service else os.environ
    print(prepare_dir(environment=environment) if args.prepare else data_dir(environment=environment))


if __name__ == "__main__":
    main()
