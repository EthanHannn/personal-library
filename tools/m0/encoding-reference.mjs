// Independent reference for the exact TextDecoder runtime used by the old app.
// Run with the legacy image's Node binary; record both Node and ICU versions.
import { createHash } from 'node:crypto'
const decoder = new TextDecoder('gb18030', { fatal: true })
const hashes = {}
for (const [kind, count] of [['pairs', 23940], ['quads', 39420]]) {
  const digest = createHash('sha256')
  for (let p = 0; p < count; p++) {
    let bytes
    if (kind === 'pairs') {
      let trail = p % 190 + 0x40
      if (trail >= 0x7f) trail++
      bytes = [Math.floor(p / 190) + 0x81, trail]
    } else {
      let n = p
      const b4 = n % 10 + 0x30; n = Math.floor(n / 10)
      const b3 = n % 126 + 0x81; n = Math.floor(n / 126)
      const b2 = n % 10 + 0x30; n = Math.floor(n / 10)
      bytes = [n + 0x81, b2, b3, b4]
    }
    digest.update(decoder.decode(Uint8Array.from(bytes)), 'utf8')
  }
  hashes[kind] = digest.digest('hex')
}
console.log(JSON.stringify({ node: process.version, icu: process.versions.icu, hashes }, null, 2))
