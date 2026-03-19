class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.5.6"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.6/jot_v1.5.6_darwin_arm64.tar.gz"
      sha256 "20d0165e253739ca84120ca41cade45614b42ec3c19d2af40b4886705482886a"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.6/jot_v1.5.6_darwin_amd64.tar.gz"
      sha256 "7a32a69d1c20efe6cf2e37fc87354011e3b197c89f930cbd5f3ba4ff132aaf7f"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.6/jot_v1.5.6_linux_amd64.tar.gz"
    sha256 "5dc2cce0fad1b7b34c18b8bb51f2e87b42242b0709ac0d0768ff9249863df693"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
