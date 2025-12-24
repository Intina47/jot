class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.5.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.1/jot_v1.5.1_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.1/jot_v1.5.1_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.1/jot_v1.5.1_linux_amd64.tar.gz"
    sha256 "REPLACE_WITH_SHA256"
  end

  def install
    bin.install "jot"
  end
end
