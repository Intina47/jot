class JotCli < Formula
  desc "Terminal-first notebook and local document viewer"
  homepage "https://github.com/Intina47/jot"
  version "1.7.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_darwin_arm64.tar.gz"
      sha256 "86974f8edf963a813fca894161391407db582f878904302542480a9a0d3f67f7"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_darwin_amd64.tar.gz"
      sha256 "a60c11de4f9f3b5a99c54b9ea4ce8a6731990dbbad2413df0ad09ee1c3d9768a"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.7.0/jot_v1.7.0_linux_amd64.tar.gz"
    sha256 "54d63d09de253747f23c809fb51dca67b457d4d0b0b9a4d89d588417b4147668"
  end

  def install
    bin.install "jot"
  end

  test do
    assert_match "jot #{version}", shell_output("#{bin}/jot help")
  end
end
