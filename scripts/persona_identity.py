#!/usr/bin/env python3
"""Resolve runtime state roots without reading build configuration or credentials."""
import argparse
import base64
import glob
import os
import pathlib
import plistlib
import re
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


def _file_environment(path):
    """Consume complete systemd-style assignments while retaining only HOME values."""
    values, key, value = {}, [], []
    state, wanted, trailing = "before_key", False, None
    whitespace, newline = " \t\n\r", "\n\r"

    def append(character):
        if wanted:
            value.append(character)

    def emit(trim=False):
        if wanted:
            result = value[:trailing] if trim and trailing is not None else value
            values["".join(key).rstrip(whitespace)] = "".join(result)

    with path.open(newline="") as stream:
        ended = False
        for line in stream:
            for character in line:
                if character == "\0":
                    ended = True
                    break
                if state == "before_key":
                    if character in "#;":
                        state = "comment"
                    elif character not in whitespace:
                        key, state = [character], "key"
                elif state == "key":
                    if character in newline:
                        key, state = [], "before_key"
                    elif character == "=":
                        wanted = "".join(key).rstrip(whitespace) in ("PERSONA_HOME", "JARVIS_HOME")
                        value, trailing, state = [], None, "before_value"
                    else:
                        key.append(character)
                elif state in ("before_value", "plain") and character in newline:
                    emit(trim=state == "plain")
                    key, value, wanted, trailing, state = [], [], False, None, "before_key"
                elif state == "before_value":
                    if character == "'":
                        state = "single"
                    elif character == '"':
                        state = "double"
                    elif character == "\\":
                        state = "plain_escape"
                    elif character not in whitespace:
                        append(character)
                        state = "plain"
                elif state == "plain":
                    if character == "\\":
                        trailing, state = None, "plain_escape"
                    else:
                        if character not in whitespace:
                            trailing = None
                        elif trailing is None:
                            trailing = len(value)
                        append(character)
                elif state == "plain_escape":
                    state = "plain"
                    if character not in newline:
                        append(character)
                elif state == "single":
                    if character == "'":
                        state = "before_value"
                    else:
                        append(character)
                elif state == "double":
                    if character == '"':
                        state = "before_value"
                    elif character == "\\":
                        state = "double_escape"
                    else:
                        append(character)
                elif state == "double_escape":
                    state = "double"
                    if character in ('"', "\\", "`", "$"):
                        append(character)
                    elif character != "\n":
                        append("\\")
                        append(character)
                elif state == "comment":
                    if character == "\\":
                        state = "comment_escape"
                    elif character in newline:
                        state = "before_key"
                elif state == "comment_escape":
                    state = "before_key" if character in newline else "comment"
            if ended:
                break
    if state in ("before_value", "plain", "plain_escape", "single", "double", "double_escape"):
        emit(trim=state == "plain")
    return values


def _unit_words(text):
    """Unquote unit words without turning C-escaped whitespace into word boundaries."""
    decoded, ordinal = {}, 0xE000
    simple = {"a": "\a", "b": "\b", "f": "\f", "n": "\n", "r": "\r", "t": "\t", "v": "\v",
              "s": " ", "e": "\x1b", "\\": "\\", '"': '"', "'": "'"}

    def protect(match):
        nonlocal ordinal
        escape = match.group(0)[1:]
        if escape in simple:
            value = simple[escape].encode("utf-8")
        elif escape[0] in "uU":
            value = chr(int(escape[1:], 16)).encode("utf-8")
        elif escape[0] == "x":
            value = bytes([int(escape[1:], 16)])
        else:
            value = bytes([int(escape, 8)])
        if b"\0" in value:
            raise ValueError("NUL is not valid in a unit environment")
        while chr(ordinal) in text:
            ordinal += 1
        marker = chr(ordinal)
        ordinal += 1
        decoded[marker] = value
        return marker

    encoded = re.sub(r"\\(?:x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}|[0-7]{3}|[abfnrtvse\\\"'])", protect, text)
    return [b"".join(decoded.get(character, character.encode("utf-8")) for character in word).decode("utf-8")
            for word in shlex.split(encoded)]


def _unit_environment(unit, home):
    values, environment_files, unset = {}, [], []
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
            setting = setting.strip()
            if not sep:
                continue
            if setting not in ("Environment", "EnvironmentFile", "UnsetEnvironment"):
                continue
            if setting == "EnvironmentFile":
                if not text.strip():
                    environment_files.clear()
                else:
                    environment_files.append(text.strip())
                continue
            try:
                entries = _unit_words(text)
            except ValueError:
                raise OSError("service environment could not be parsed; set PERSONA_HOME explicitly") from None
            if setting == "UnsetEnvironment":
                if not entries:
                    unset.clear()
                unset.extend(entries)
                continue
            if not entries:
                values.clear()
            for entry in entries:
                key, sep, value = entry.partition("=")
                if sep and key in ("PERSONA_HOME", "JARVIS_HOME"):
                    values[key] = value.replace("%%", "\0").replace("%h", home).replace("\0", "%")
    for entry in environment_files:
        optional = entry.startswith("-")
        pattern = entry[1:] if optional else entry
        pattern = pattern.replace("%%", "\0").replace("%h", home).replace("\0", "%")
        if not os.path.isabs(pattern):
            raise OSError("EnvironmentFile must name an absolute path")
        matches = sorted(glob.glob(pattern))
        if not matches and not optional:
            raise FileNotFoundError("required service environment file is unavailable")
        for path in matches:
            try:
                values.update(_file_environment(pathlib.Path(path)))
            except (OSError, UnicodeError):
                if not optional:
                    raise
    for entry in unset:
        entry = entry.replace("%%", "\0").replace("%h", home).replace("\0", "%")
        key, sep, value = entry.partition("=")
        if key in values and (not sep or values[key] == value):
            del values[key]
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
        # The resolver travels base64-encoded on the python3 -c line: a heredoc inside $(...) breaks macOS's bash 3.2,
        # which mis-parses the backticks and quotes in the code ("unexpected EOF while looking for matching `").
        code = base64.b64encode(pathlib.Path(__file__).read_bytes()).decode()
        print("_PERSONA_STATE_DIR=$(python3 -c \"import base64; exec(base64.b64decode('" + code + "'))\" --service "
              + (args.service or "auto") + (" --prepare" if args.prepare else "") + ") || exit $?")
        print('export PERSONA_HOME="$_PERSONA_STATE_DIR" JARVIS_HOME="$_PERSONA_STATE_DIR"')
        if args.prefix != "-": print(pathlib.Path(args.prefix).read_text(), end="")
        return
    environment = service_environment(role=args.service) if args.service else os.environ
    print(prepare_dir(environment=environment) if args.prepare else data_dir(environment=environment))


if __name__ == "__main__":
    main()
