/* eslint-disable no-console */
"use strict";

const fs = require("fs");
const https = require("https");
const path = require("path");
const { execFileSync } = require("child_process");

const pkg = require("./package.json");
const releaseVersion = pkg.jotReleaseVersion || pkg.version;
const tag = `v${releaseVersion}`;

const platform = process.platform;
const arch = process.arch;

const isWindows = platform === "win32";
const isMac = platform === "darwin";
const isLinux = platform === "linux";

if (!["x64", "arm64"].includes(arch)) {
  console.error(`Unsupported arch: ${arch}`);
  process.exit(1);
}

if (!isWindows && !isMac && !isLinux) {
  console.error(`Unsupported platform: ${platform}`);
  process.exit(1);
}

let goos = platform;
if (isMac) {
  goos = "darwin";
}

let goarch = arch === "x64" ? "amd64" : "arm64";

if (isWindows && goarch === "arm64") {
  console.error("Windows arm64 binary is not published yet.");
  process.exit(1);
}

const assetName = isWindows
  ? `jot_${tag}_windows_amd64.zip`
  : `jot_${tag}_${goos}_${goarch}.tar.gz`;
const url = `https://github.com/Intina47/jot/releases/download/${tag}/${assetName}`;

const binDir = path.join(__dirname, "bin");
const binName = isWindows ? "jot.exe" : "jot";
const binPath = path.join(binDir, binName);
const archivePath = path.join(binDir, assetName);

fs.mkdirSync(binDir, { recursive: true });

function download(fileUrl, dest, cb, redirectsLeft = 5) {
  const file = fs.createWriteStream(dest);
  https
    .get(fileUrl, (res) => {
      if ([301, 302, 303, 307, 308].includes(res.statusCode)) {
        if (!res.headers.location || redirectsLeft <= 0) {
          console.error(`Too many redirects for ${fileUrl}`);
          process.exit(1);
        }
        file.close(() => {
          fs.unlinkSync(dest);
          const nextUrl = res.headers.location.startsWith("http")
            ? res.headers.location
            : new URL(res.headers.location, fileUrl).toString();
          download(nextUrl, dest, cb, redirectsLeft - 1);
        });
        return;
      }

      if (res.statusCode !== 200) {
        console.error(`Failed to download ${fileUrl} (status ${res.statusCode})`);
        process.exit(1);
      }
      res.pipe(file);
      file.on("finish", () => file.close(cb));
    })
    .on("error", (err) => {
      console.error(`Download error: ${err.message}`);
      process.exit(1);
    });
}

function extract() {
  if (isWindows) {
    execFileSync("powershell", [
      "-NoProfile",
      "-Command",
      `Expand-Archive -Path "${archivePath}" -DestinationPath "${binDir}" -Force`,
    ]);
  } else {
    execFileSync("tar", ["-xzf", archivePath, "-C", binDir]);
    fs.chmodSync(binPath, 0o755);
  }
}

download(url, archivePath, () => {
  extract();
  fs.unlinkSync(archivePath);
});
