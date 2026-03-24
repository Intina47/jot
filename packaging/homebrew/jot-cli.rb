class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.6.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.6.0/jot_v1.6.0_darwin_arm64.tar.gz"
      sha256 "e7cb04b8560e78f97314e0cd789595e5955cabd1c4f03a9ec68cd14dc806e01d"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.6.0/jot_v1.6.0_darwin_amd64.tar.gz"
      sha256 "575ae284a7bf61dcea303c9a64fa49c799e51a85c03ac135955fa60524a3d7af"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.6.0/jot_v1.6.0_linux_amd64.tar.gz"
    sha256 "d05ea59464a5df5f9e2ab2cbb1352871ec3ec4d1cda16334f5941420fc5fe6c9"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
