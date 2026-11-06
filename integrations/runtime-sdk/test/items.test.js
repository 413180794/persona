import { test } from 'node:test'
import assert from 'node:assert/strict'
import { summaryOf } from '../index.js'

test('summaryOf says what a call does, not just which tool', () => {
  assert.equal(summaryOf('bash', { command: 'rm -rf build', description: 'clean' }), 'rm -rf build', 'what runs comes first')
  assert.equal(summaryOf('Agent', { description: 'Find the flaky test', prompt: 'long…' }), 'Find the flaky test')
  assert.equal(summaryOf('mcp__lark__send', { chat_id: 'oc_1', message: 'hello there' }), 'hello there')
  assert.equal(summaryOf('mcp__x__create', { repo: 'a/b', count: 3 }), 'repo: a/b', 'any other text, with its name')
  assert.equal(summaryOf('mcp__x__ping', { count: 3 }), 'mcp__x__ping', 'no text at all: the tool name')
  assert.equal(summaryOf('mcp__x__ping', { path: '  ', count: 3 }), 'mcp__x__ping', 'blank strings say nothing')
  assert.equal(summaryOf('t', null), 't')
  assert.equal(summaryOf('t', ['a']), 't')
  assert.equal([...summaryOf('t', { text: 'x'.repeat(300) })].length, 201, 'one line, cut')
  assert.equal(summaryOf('x', { api_key: 'sk-live-123', repo: 'a' }), 'repo: a', 'a credential is skipped')
  for (const k of ['token', 'api_key', 'apiKey', 'APIKey', 'x-api-key', 'authorization', 'Authorization', 'password', 'client_secret', 'cookie', 'credentials',
    'session_id', 'sessionId', 'private_key', 'headers', 'bearer', 'Bearer', 'totp', 'pin', 'dsn', 'database_dsn', 'connection_string', 'connStr', 'webhook_url', 'slackWebhook', 'pwd', 'pass', 'jwt', 'access_key', 'accessKeyId', 'key', 'signature', 'auth',
    'x-auth', 'auth_code', 'otp', 'SSHKey', 'GPGKey', 'AWSKey', 'DBPass', 'HTTPAuth', 'MFAPin', 'PGPKeys']) {
    assert.equal(summaryOf('mcp__x__call', { [k]: 'secret-value' }), 'mcp__x__call', `${k} alone: the tool name`)
  }
  assert.equal(summaryOf('x', { password: 'hunter2', user: 'bob' }), 'user: bob')
  for (const k of ['author', 'authorName', 'oauth_scope', 'session_name', 'keyword', 'passage', 'monkey', 'OAuthScope', 'URLPath', 'HTTPMethod', 'IDList']) {
    assert.equal(summaryOf('x', { [k]: 'v' }), `${k}: v`, `${k} is no credential: shown`)
  }
})
