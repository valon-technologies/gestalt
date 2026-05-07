# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.19"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-macos-arm64.tar.gz"
      sha256 "93067318c9e153f68f5f74c351a7e3fc168fcf21d0d5461b7f024fa94fa450d0"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-macos-x86_64.tar.gz"
      sha256 "77e73ff50361a17ac1051dc0c7deff68b60a0374ad913db4ec71b7a4190a13de"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-linux-arm64.tar.gz"
      sha256 "a314c68b47996323a11c5807bf98e531362f1a9ba7cb769f41e5ba997a204024"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.19/gestaltd-linux-x86_64.tar.gz"
      sha256 "a3f5c892ed34dc3116d88f1e27137a51df8c91c65edf9c85bd09de7e2f00a892"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
