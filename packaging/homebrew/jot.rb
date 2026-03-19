class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.5.4"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_darwin_arm64.tar.gz"
      sha256 "ef73e597a467a213aae07051e006dabab15553cc36597296f77d1104c1208f2c"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_darwin_amd64.tar.gz"
      sha256 "5271482f0cb217a8c5b2fbe5bc3ea6ffa2cced9d2fc3755e23c6a4f90f67fdc7"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_linux_amd64.tar.gz"
    sha256 "a968fe0c24a15e739a8b9d7ee979747b1ed9c55bbe02a2b260922b30713d9403"
  end

  def install
    bin.install "jot"
  end
end
