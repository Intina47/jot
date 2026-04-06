class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.7.2"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.7.2/jot_v1.7.2_darwin_arm64.tar.gz"
      sha256 "bef70d22f14511715ec2a1821d376b0b95a82a7ab031c72d2b589a0a554b1914"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.7.2/jot_v1.7.2_darwin_amd64.tar.gz"
      sha256 "848cca6beb3a57e18053087729f22af55c3b21b3b8769b428b75f1f7d63345ae"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.7.2/jot_v1.7.2_linux_amd64.tar.gz"
    sha256 "2678ba0645de9014ea99473a66b805635e90fcc09af5c7cded11526b266280f2"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
