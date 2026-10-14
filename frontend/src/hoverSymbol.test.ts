import { test } from 'node:test'
import assert from 'node:assert/strict'
import { hoverSymbolSpan } from './hoverSymbol.ts'

test('hover positions inside one PHP variable or method share a UTF-16 span', () => {
  const line = '$greeter->greet($name);'
  assert.deepEqual(hoverSymbolSpan(line, 1), [0, 8])
  assert.deepEqual(hoverSymbolSpan(line, 6), [0, 8])
  assert.deepEqual(hoverSymbolSpan(line, 11), [10, 15])
  assert.deepEqual(hoverSymbolSpan(line, 14), [10, 15])
  assert.equal(hoverSymbolSpan(line, 8), undefined)
  assert.equal(hoverSymbolSpan(line, 15), undefined)
})

test('symbol spans preserve Unicode and offsets after astral characters', () => {
  const line = '"😀"; const café = π + 𐐀name;'
  for (const symbol of ['café', 'π', '𐐀name']) {
    const start = line.indexOf(symbol)
    assert.deepEqual(hoverSymbolSpan(line, start), [start, start + symbol.length])
    assert.deepEqual(hoverSymbolSpan(line, start + symbol.length - 1), [start, start + symbol.length])
  }
  assert.equal(hoverSymbolSpan(line, line.indexOf(' ')), undefined)
})
