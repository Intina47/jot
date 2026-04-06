class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.7.3"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.7.3/jot_v1.7.3_darwin_arm64.tar.gz"
      sha256 "fdd8cfe392a7f9b321c4b2c654f89b3167892c3aef0d21a129fe90dc64e4bcbc"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.7.3/jot_v1.7.3_darwin_amd64.tar.gz"
      sha256 "1f2a724e9b38718054f2ab571aa3204ff42d36d46065855436e00b39f117acf0"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.7.3/jot_v1.7.3_linux_amd64.tar.gz"
    sha256 "6823cb48ca408db21d5c1458e806791fb2bd220065d999810807f8af6d514e3e"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
