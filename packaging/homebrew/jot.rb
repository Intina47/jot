class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.0.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.0.0/jot_v1.0.0_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.0.0/jot_v1.0.0_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.0.0/jot_v1.0.0_linux_amd64.tar.gz"
    sha256 "REPLACE_WITH_SHA256"
  end

  def install
    bin.install "jot"
  end
end
