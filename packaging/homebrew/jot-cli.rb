class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.5.7"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.7/jot_v1.5.7_darwin_arm64.tar.gz"
      sha256 "00c0bd669a3ad9fcee17961ebd8117b0365a978d3bb3506f046e2fb17310c8d5"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.7/jot_v1.5.7_darwin_amd64.tar.gz"
      sha256 "dbc823e0d5d87274c5287d13bdb267bd0d4d252c06358e5e219630149eb59136"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.7/jot_v1.5.7_linux_amd64.tar.gz"
    sha256 "7ab076c31020579145a16ea24a1ad8876becfedc54a7bfb613c11540de22a15a"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
