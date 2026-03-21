class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.5.8"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_darwin_arm64.tar.gz"
      sha256 "e695b4133f952b04ee0371233f3d0620825e59c881816131893ff711bbfe8f36"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_darwin_amd64.tar.gz"
      sha256 "c4b3e53f74bb60b31100cc7cda066358f027287b6cf0d46e2944ddd914405f4c"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_linux_amd64.tar.gz"
    sha256 "63cdd2ea6f66116ee859a3e3e4ce8b749d33cb51455c0ce6bb06b1138c6c6780"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
