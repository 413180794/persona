"""Child-process programs for isolated state-directory migration tests."""

GO_MAIN = r'''
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

func main() {
    home := os.Getenv("HOME")
    mode := os.Getenv("EDGE_MODE")
    cached := DataDir(home)
    gate := os.Getenv("EDGE_GATE")
    if gate != "" {
        if err := os.WriteFile(os.Getenv("EDGE_READY"), []byte("ready"), 0600); err != nil { panic(err) }
        deadline := time.Now().Add(10 * time.Second)
        for {
            if _, err := os.Stat(gate); err == nil { break }
            if time.Now().After(deadline) { panic("gate timeout") }
            time.Sleep(time.Millisecond)
        }
    }
    root := cached
    var err error
    if mode != "query" { root, err = PrepareHome(home) }
    out := map[string]string{"cached": cached, "root": root, "query": DataDir(home)}
    if err != nil {
        out["error"] = err.Error()
    } else if mode == "write" {
        if err := os.WriteFile(filepath.Join(root, "go-state.fixture"), []byte("go-payload"), 0600); err != nil {
            out["error"] = err.Error()
        }
    }
    data, _ := json.Marshal(out)
    fmt.Println(string(data))
}
'''

NODE_INSTALL = r'''
import { writeFileSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { setTimeout } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';
const { prepareSocketDirectory, stateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
const { defaultSocketPath } = await import(pathToFileURL(process.env.EDGE_RUNTIME));
const { installCurrent, packageHash } = await import(pathToFileURL(process.env.EDGE_PACKAGE));
const socket = defaultSocketPath();
const files = [{ path: 'pi/index.js', data: Buffer.from('export const fixture = true;\n') }];
const hash = packageHash(files);
writeFileSync(process.env.EDGE_READY, 'ready');
const deadline = Date.now() + 10000;
while (!existsSync(process.env.EDGE_GATE)) {
  if (Date.now() >= deadline) throw new Error('gate timeout');
  await setTimeout(1);
}
try {
  const root = prepareSocketDirectory(socket);
  const store = join(root, 'runtime-packages', 'persona-pi-runtime');
  const installed = installCurrent(store, hash, files);
  console.log(JSON.stringify({ cached: dirname(socket), root, query: stateDir(), installed, hash }));
} catch (err) {
  console.log(JSON.stringify({ cached: dirname(socket), query: stateDir(), error: String(err) }));
}
'''

NODE_QUERY = r'''
import { pathToFileURL } from 'node:url';
const { stateDir } = await import(pathToFileURL(process.env.EDGE_IDENTITY));
const { defaultSocketPath, createRuntime } = await import(pathToFileURL(process.env.EDGE_RUNTIME));
const root = stateDir();
const socket = defaultSocketPath();
if (process.env.EDGE_DAEMON) {
  createRuntime({ listSessions: async () => [], items: async () => ({ items: [] }) },
                { runtime: 'migration-edge-fixture', listen: true, token: 'fixture-only' });
}
console.log(JSON.stringify({ root, socket }));
'''

