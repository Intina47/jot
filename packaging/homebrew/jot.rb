class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.5.4"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_darwin_arm64.tar.gz"
      sha256 "4e8a4d3fd02a0aa59dda7ba0efa9311eb52ba7a857676f67ac9ccf7594a2e9f2"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_darwin_amd64.tar.gz"
      sha256 "4b8de503b1e069952da407a3a1f4aefd90172de65c722f2fdcce89fb78a950f7"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.4/jot_v1.5.4_linux_amd64.tar.gz"
    sha256 "82e336fb96e7afd7d66f6f5277912ec20c1464d7c5eca01bc1d24b4ddcf46bf2"
  end

  def install
    bin.install "jot"
  end
end
