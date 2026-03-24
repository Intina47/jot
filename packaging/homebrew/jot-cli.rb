class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.6.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.6.1/jot_v1.6.1_darwin_arm64.tar.gz"
      sha256 "fcb76bed728ac86e4770bbd0518b1b0c19f050c061c38f1267b3f279d2d9d30c"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.6.1/jot_v1.6.1_darwin_amd64.tar.gz"
      sha256 "6169e000e86bb49e97b3e3130d28cd6774131ecf46ae838009b20d0b9ab49ce3"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.6.1/jot_v1.6.1_linux_amd64.tar.gz"
    sha256 "b0d77971191478374b59b307142fa4aac172105fd6b2dff9db1256b8e7bbba89"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
