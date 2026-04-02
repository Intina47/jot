class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.7.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_darwin_arm64.tar.gz"
      sha256 "c5c78b9c2061d37c9b7b380e2d1b275cd48c5e4336d04c9dc95a4106736d468f"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_darwin_amd64.tar.gz"
      sha256 "c5602518ac977a893988f9dc5e7339c5ac20d0e293a0b3e3f8587146c767d2fa"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_linux_amd64.tar.gz"
    sha256 "630185c8a9811d619545a2e47117c54f1529348ee0d7dacb290d8cf9793f7ff4"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
