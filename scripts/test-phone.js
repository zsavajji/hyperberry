// Checks app/assets/phone.js on the PC: node scripts/test-phone.js
// The QML-only line (.pragma) is dropped.
const fs = require("fs");
const assert = require("assert");
const dir = __dirname + "/../app/assets/";
const qmlJs = (f) => fs.readFileSync(dir + f, "utf8").replace(/^\.(pragma|import).*$/gm, "");
const Countries = new Function(qmlJs("countries.js") + "; return {data: data};")();
const Phone = new Function(qmlJs("phone.js") +
    "; return {all: all, byRegion: byRegion, flag: flag, detect: detect, international: international};")();

const en = Phone.all(Countries.data, "en"), it = Phone.all(Countries.data, "it");
assert.strictEqual(en.length, 245);
assert.strictEqual(Phone.byRegion(it, "IT").name, "Italia");
assert.strictEqual(Phone.flag("IT"), "\u{1F1EE}\u{1F1F9}");

const region = (digits, home) => { const d = Phone.detect(en, digits, home); return d && d.country.region + " " + d.rest; };
assert.strictEqual(region("393331234567", "IT"), "IT 3331234567");
assert.strictEqual(region("3", "IT"), null);                 // not a whole code yet
assert.strictEqual(region("39", "IT"), "IT ");              // a whole code, nothing after it
assert.strictEqual(region("12125551234", "IT"), "US 2125551234"); // +1: the main region
assert.strictEqual(region("14165551234", "CA"), "CA 4165551234"); // +1 on a Canadian phone
assert.strictEqual(region("447911123456", "IT"), "GB 7911123456");
assert.strictEqual(region("3784", "IT"), "SM 4");          // three digits

const country = (r) => Phone.byRegion(en, r);
assert.strictEqual(Phone.international(country("IT"), "333 123 4567"), "+393331234567");
assert.strictEqual(Phone.international(country("IT"), "06 1234567"), "+39061234567"); // Italy keeps its 0
assert.strictEqual(Phone.international(country("GB"), "07911 123456"), "+447911123456");
assert.strictEqual(Phone.international(country("RU"), "8 916 123-45-67"), "+79161234567");
console.log("phone.js: ok");
