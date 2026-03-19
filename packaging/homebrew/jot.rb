class Jot < Formula
  desc "Terminal-first notebook for nonsense"
  homepage "https://github.com/Intina47/jot"
  version "1.5.3"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Intina47/jot/releases/download/v1.5.3/jot_v1.5.3_darwin_arm64.tar.gz"
      sha256 "863f2967b67e1ab3e99d35b85e9c546a07dc480481769e4adfbbdc3969a419a6"
    else
      url "https://github.com/Intina47/jot/releases/download/v1.5.3/jot_v1.5.3_darwin_amd64.tar.gz"
      sha256 "327b38631d9b54e2012d9910140cc69050d58fb7199b8a29e237df2e02927681"
    end
  end

  on_linux do
    url "https://github.com/Intina47/jot/releases/download/v1.5.3/jot_v1.5.3_linux_amd64.tar.gz"
    sha256 "897bb60cd011762fb40d25edbda991e2f47b7e9bd57d7d72406094ec39fb7f8d"
  end

  def install
    bin.install "jot"
  end
end
