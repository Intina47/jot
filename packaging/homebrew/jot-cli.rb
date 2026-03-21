class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.5.8"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_darwin_arm64.tar.gz"
      sha256 "4539e15320b165204e796097933631fbf48719aaaf47afea63bd3d7f6c5a96d7"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_darwin_amd64.tar.gz"
      sha256 "0dc4dd83193f02055672c6b7687fa853cc21c0e97d786a85224aca55607a470e"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.8/jot_v1.5.8_linux_amd64.tar.gz"
    sha256 "8716aff0bc170419c25fda2b794d1b3c9fb2455ff87dffd2633ba0921a85acad"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
