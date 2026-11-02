// Regenerate checked-in Windows resources from the same SVG used by the UI.
// SHARP_MODULE and GO_BINARY may point to locally installed build tools.
const fs = require('node:fs/promises');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const sharp = require(process.env.SHARP_MODULE || 'sharp');
const root = path.resolve(__dirname, '..');

async function main() {
  const svg = await fs.readFile(path.join(root, 'web/public/logo.svg'));
  const sizes = [16, 20, 24, 32, 40, 48, 64, 96, 128, 256];
  const images = [];
  for (const size of sizes) {
    const renderer = sharp(svg, { density: 768 }).resize(size, size);
    if (size === 256) {
      images.push(await renderer.png().toBuffer());
      continue;
    }
    // ICO DIBs use bottom-up BGRA pixels plus a DWORD-aligned transparency mask.
    const rgba = await renderer.ensureAlpha().raw().toBuffer();
    const maskStride = Math.ceil(size / 32) * 4;
    const bitmap = Buffer.alloc(40 + size * size * 4 + maskStride * size);
    bitmap.writeUInt32LE(40, 0);
    bitmap.writeInt32LE(size, 4);
    bitmap.writeInt32LE(size * 2, 8);
    bitmap.writeUInt16LE(1, 12);
    bitmap.writeUInt16LE(32, 14);
    bitmap.writeUInt32LE(size * size * 4, 20);
    for (let y = 0; y < size; y++) {
      for (let x = 0; x < size; x++) {
        const source = (y * size + x) * 4;
        const target = 40 + ((size - 1 - y) * size + x) * 4;
        bitmap[target] = rgba[source + 2];
        bitmap[target + 1] = rgba[source + 1];
        bitmap[target + 2] = rgba[source];
        bitmap[target + 3] = rgba[source + 3];
        if (rgba[source + 3] === 0) {
          bitmap[40 + size * size * 4 + (size - 1 - y) * maskStride + (x >> 3)] |= 0x80 >> (x & 7);
        }
      }
    }
    images.push(bitmap);
  }
  const directory = Buffer.alloc(6 + sizes.length * 16);
  directory.writeUInt16LE(1, 2);
  directory.writeUInt16LE(sizes.length, 4);
  let offset = directory.length;
  for (let i = 0; i < sizes.length; i++) {
    const entry = 6 + i * 16;
    directory[entry] = directory[entry + 1] = sizes[i] % 256;
    directory.writeUInt16LE(1, entry + 4);
    directory.writeUInt16LE(32, entry + 6);
    directory.writeUInt32LE(images[i].length, entry + 8);
    directory.writeUInt32LE(offset, entry + 12);
    offset += images[i].length;
  }
  await fs.writeFile(path.join(root, 'deploy/windows/app.ico'), Buffer.concat([directory, ...images]));
  await fs.writeFile(path.join(root, 'website/favicon.svg'), svg);
  execFileSync(process.env.GO_BINARY || 'go', [
    'run', 'github.com/akavel/rsrc@v0.10.2', '-ico', 'deploy/windows/app.ico',
    '-arch', 'amd64', '-o', 'cmd/dns-assistant/resource_windows_amd64.syso',
  ], { cwd: root, stdio: 'inherit' });
  console.log('Generated Windows icon resources and favicon from web/public/logo.svg.');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
