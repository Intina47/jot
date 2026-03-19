class Jot < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.5.5"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.5/jot_v1.5.5_darwin_arm64.tar.gz"
      sha256 "c17b20056c0d5fa3d1c5e3bdb607265f55694ca89cf68ac696a662d73841d373"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.5/jot_v1.5.5_darwin_amd64.tar.gz"
      sha256 "aaa0dc1ceeca2f22213601a2578a9673bf0933ea2b91d81c6bc087d69708d0f2"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.5/jot_v1.5.5_linux_amd64.tar.gz"
    sha256 "2022f10022ebf89555ae3125eb6aac9abdd3f696ae4ec1a5c0b97ee09221e1c3"
  end

  def install
    bin.install "jot"
  end
end
