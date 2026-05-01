# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.7"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.7/gestaltd-macos-arm64.tar.gz"
      sha256 "51d3de69514c953c90807b5ea049e631d025c3d97857ea97781ca9d6a163d0d6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.7/gestaltd-macos-x86_64.tar.gz"
      sha256 "96394dfbd73a64f35271d1cc7b5bacff78200a0dc5dc844eb9ca77bbdf6fb702"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.7/gestaltd-linux-arm64.tar.gz"
      sha256 "85db31545ea5b031461b7f0630b41754b07680a6999660df6d89d7c98a7ab91c"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.7/gestaltd-linux-x86_64.tar.gz"
      sha256 "0ae87185770cd0d9abaa9ff9ce48a7d502a4f2f9ba8d24e57e9b0bcb76505e6c"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
