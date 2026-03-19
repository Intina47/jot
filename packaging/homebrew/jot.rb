class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.5.2"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.2/jot_v1.5.2_darwin_arm64.tar.gz"
      sha256 "9229a46c7024c99f073fac8aea28a45f5a814c0180b618d76aed4fff0d81eac0"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.2/jot_v1.5.2_darwin_amd64.tar.gz"
      sha256 "2dfed2ee2e436708f2194ca1f3385956fa1618cb848d45726ae738bbb0c55dab"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.2/jot_v1.5.2_linux_amd64.tar.gz"
    sha256 "d9593026b15cf4a8cddaf81db0721c10eb50a4570459fb9c5806c39ea2b38f98"
  end

  def install
    bin.install "jot"
  end
end
